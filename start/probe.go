package start

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/flanksource/deps/pkg/types"
)

// Readiness reports what awaitHealthy is currently blocked on, so callers can
// show progress instead of an opaque wait.
type Readiness struct {
	Service string
	// Waiting describes the unmet condition, e.g. `tcp 127.0.0.1:5432` or
	// `"ready to accept connections" in process output`.
	Waiting string
	Elapsed time.Duration
	// Ports are the ports the process was observed listening on, when the
	// runtime can detect them (binary runtime).
	Ports []int
}

// processWatch lets awaitHealthy observe an in-process service (binary
// runtime): alive reports whether it is still running/starting and output
// returns its combined stdout/stderr.
type processWatch struct {
	alive  func() bool
	output func() string
	// execProbe runs an exec-style health command in the service's context
	// (docker runtime); nil means exec probes are unsupported.
	execProbe func(ctx context.Context, cmd []string) bool
	// ports returns the ports the service was detected listening on; nil
	// means the runtime cannot detect them.
	ports func() []int
}

func (w *processWatch) detectedPorts() []int {
	if w == nil || w.ports == nil {
		return nil
	}
	return w.ports()
}

func (w *processWatch) tailOutput(n int) string {
	if w == nil || w.output == nil {
		return ""
	}
	return tail(w.output(), n)
}

// probeStage is one ordered readiness condition.
type probeStage struct {
	desc  string
	check func() bool
}

// logOffset bounds the output a stdout probe may match: the size a service's
// log file had before the start or restart being waited on. Without it an
// earlier run's "ready" line would satisfy the check immediately, and a
// restart driven from another process has no other view of the output.
type logOffset struct {
	path   string
	offset int64
}

func logOffsetOf(path string) logOffset {
	if path == "" {
		return logOffset{}
	}
	var size int64
	if info, err := os.Stat(path); err == nil {
		size = info.Size()
	}
	return logOffset{path: path, offset: size}
}

// read returns the log written since the offset was recorded.
func (l logOffset) read() string {
	if l.path == "" {
		return ""
	}
	f, err := os.Open(l.path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(l.offset, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return string(data)
}

// healthCheckFor resolves the effective health check: the runtime-specific
// override, else the service default, else a TCP wait on the primary port.
func healthCheckFor(svc *ServiceContext, override *types.HealthCheck) types.HealthCheck {
	if override != nil {
		return *override
	}
	if svc.Spec.Health != nil {
		return *svc.Spec.Health
	}
	return types.HealthCheck{}
}

func parseDuration(value string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", value, err)
	}
	return d, nil
}

// probePort resolves the host port the health check targets.
func probePort(svc *ServiceContext, hc types.HealthCheck) (int, error) {
	primary, ok := svc.Spec.PrimaryPort()
	if !ok {
		return 0, fmt.Errorf("service %s declares no ports", svc.Name)
	}
	if hc.Port == "" || hc.Port == primary.Name {
		return hostPort(svc), nil
	}
	// configuredPorts maps every declared port name to the port it is
	// published on, which is what a probe running on the host must dial.
	if port, ok := configuredPorts(svc)[hc.Port]; ok {
		return port, nil
	}
	return 0, fmt.Errorf("health check references unknown port %q", hc.Port)
}

// awaitHealthy blocks until the service passes every readiness stage in
// order. watch, when non-nil, is observed for stdout_match, detected ports
// and early exit.
func awaitHealthy(ctx context.Context, svc *ServiceContext, override *types.HealthCheck, watch *processWatch) error {
	hc := healthCheckFor(svc, override)
	timeout, err := parseDuration(hc.Timeout, svc.Opts.WaitTimeout)
	if err != nil {
		return err
	}
	interval, err := parseDuration(hc.Interval, 2*time.Second)
	if err != nil {
		return err
	}

	stages, err := probeStages(ctx, svc, hc, watch)
	if err != nil {
		return err
	}

	started := time.Now()
	deadline := started.Add(timeout)
	for _, stage := range stages {
		for !stage.check() {
			if watch != nil && watch.alive != nil && !watch.alive() {
				return fmt.Errorf("process exited before becoming ready:\n%s", watch.tailOutput(20))
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout after %s waiting for %s", timeout, stage.desc)
			}
			reportWaiting(svc, stage.desc, started, watch)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
			}
		}
	}
	return nil
}

// probeStages builds the ordered readiness conditions: the spec's health
// check, then — because stdout_match and exec probes can pass before the
// socket accepts — a TCP wait on the primary port.
func probeStages(ctx context.Context, svc *ServiceContext, hc types.HealthCheck, watch *processWatch) ([]probeStage, error) {
	var port int
	if hc.StdoutMatch == "" && len(hc.Exec) == 0 {
		resolved, err := probePort(svc, hc)
		if err != nil {
			return nil, err
		}
		port = resolved
	}
	spec, err := buildProbe(ctx, hc, svc.serviceHost(), port, watch)
	if err != nil {
		return nil, err
	}
	stages := []probeStage{spec}
	if gate, ok := portGate(svc); ok && gate.desc != spec.desc {
		stages = append(stages, gate)
	}
	return stages, nil
}

// portGate waits for the primary port to accept a connection.
func portGate(svc *ServiceContext) (probeStage, bool) {
	if _, ok := svc.Spec.PrimaryPort(); !ok {
		return probeStage{}, false
	}
	addr := fmt.Sprintf("%s:%d", svc.serviceHost(), hostPort(svc))
	return probeStage{desc: "tcp " + addr, check: func() bool { return tcpProbe(addr) }}, true
}

func reportWaiting(svc *ServiceContext, waiting string, started time.Time, watch *processWatch) {
	if svc.Opts.OnWaiting == nil {
		return
	}
	svc.Opts.OnWaiting(Readiness{
		Service: svc.Name,
		Waiting: waiting,
		Elapsed: time.Since(started).Truncate(time.Second),
		Ports:   watch.detectedPorts(),
	})
}

func buildProbe(ctx context.Context, hc types.HealthCheck, host string, port int, watch *processWatch) (probeStage, error) {
	switch {
	case hc.StdoutMatch != "":
		if watch == nil || watch.output == nil {
			return probeStage{}, fmt.Errorf("stdout_match health check requires the binary runtime")
		}
		return probeStage{
			desc:  fmt.Sprintf("%q in process output", hc.StdoutMatch),
			check: func() bool { return strings.Contains(watch.output(), hc.StdoutMatch) },
		}, nil
	case len(hc.Exec) > 0:
		if watch == nil || watch.execProbe == nil {
			return probeStage{}, fmt.Errorf("exec health check requires the docker runtime")
		}
		return probeStage{
			desc:  fmt.Sprintf("exec %s", strings.Join(hc.Exec, " ")),
			check: func() bool { return watch.execProbe(ctx, hc.Exec) },
		}, nil
	case hc.HTTP != "":
		url := fmt.Sprintf("http://%s:%d%s", host, port, hc.HTTP)
		return probeStage{desc: url, check: func() bool { return httpProbe(url) }}, nil
	default:
		addr := fmt.Sprintf("%s:%d", host, port)
		return probeStage{desc: "tcp " + addr, check: func() bool { return tcpProbe(addr) }}, nil
	}
}

func tcpProbe(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func httpProbe(url string) bool {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// tail returns the last n lines of output.
func tail(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
