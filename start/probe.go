package start

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/deps/pkg/types"
)

// processWatch lets awaitHealthy observe an in-process service (binary
// runtime): alive reports whether it is still running/starting and output
// returns its combined stdout/stderr.
type processWatch struct {
	alive  func() bool
	output func() string
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
	name := hc.Port
	if name == "" || name == primary.Name {
		return hostPort(svc), nil
	}
	for _, p := range svc.Spec.Ports {
		if p.Name == name {
			return p.Port, nil
		}
	}
	return 0, fmt.Errorf("health check references unknown port %q", name)
}

// awaitHealthy blocks until the service passes its health check. watch, when
// non-nil, is observed for stdout_match and early exit (binary runtime).
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

	port, err := probePort(svc, hc)
	if err != nil {
		return err
	}

	check, desc, err := buildProbe(hc, port, watch)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return nil
		}
		if watch != nil && !watch.alive() {
			return fmt.Errorf("process exited before becoming ready:\n%s", tail(watch.output(), 20))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s waiting for %s", timeout, desc)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

func buildProbe(hc types.HealthCheck, port int, watch *processWatch) (func() bool, string, error) {
	switch {
	case hc.StdoutMatch != "":
		if watch == nil {
			return nil, "", fmt.Errorf("stdout_match health check requires the binary runtime")
		}
		return func() bool { return strings.Contains(watch.output(), hc.StdoutMatch) },
			fmt.Sprintf("%q in process output", hc.StdoutMatch), nil
	case hc.HTTP != "":
		url := fmt.Sprintf("http://localhost:%d%s", port, hc.HTTP)
		return func() bool { return httpProbe(url) }, url, nil
	default:
		addr := fmt.Sprintf("localhost:%d", port)
		return func() bool { return tcpProbe(addr) }, "tcp " + addr, nil
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
