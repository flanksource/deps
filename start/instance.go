package start

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/start/state"
)

// Instance is a started (or previously started) service.
type Instance struct {
	Name       string            `json:"name" yaml:"name"`
	Runtime    RuntimeKind       `json:"runtime" yaml:"runtime"`
	Connection models.Connection `json:"connection" yaml:"connection"`
	Action     string            `json:"action,omitempty" yaml:"action,omitempty"`
	Change     *ConfigChange     `json:"-" yaml:"-"`

	State   *state.State `json:"-" yaml:"-"`
	runtime Runtime
	// opts are the resolved options the instance was created with; restart
	// reuses them so it probes and reports exactly like start did.
	opts Options
}

type ServiceInfo struct {
	Name        string            `json:"name" yaml:"name"`
	Runtime     RuntimeKind       `json:"runtime" yaml:"runtime"`
	Action      string            `json:"action,omitempty" yaml:"action,omitempty"`
	Status      state.Status      `json:"status" yaml:"status"`
	Version     string            `json:"version,omitempty" yaml:"version,omitempty"`
	Image       string            `json:"image,omitempty" yaml:"image,omitempty"`
	Chart       string            `json:"chart,omitempty" yaml:"chart,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Ports       map[string]int    `json:"ports,omitempty" yaml:"ports,omitempty"`
	Volume      *state.Volume     `json:"volume,omitempty" yaml:"volume,omitempty"`
	PID         int               `json:"pid,omitempty" yaml:"pid,omitempty"`
	ContainerID string            `json:"container_id,omitempty" yaml:"container_id,omitempty"`
	HelmRelease string            `json:"helm_release,omitempty" yaml:"helm_release,omitempty"`
	Namespace   string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Paths       ServicePaths      `json:"paths" yaml:"paths"`
	Connection  models.Connection `json:"connection" yaml:"connection"`
}

type ServicePaths struct {
	State string `json:"state" yaml:"state"`
	Run   string `json:"run" yaml:"run"`
	Data  string `json:"data" yaml:"data"`
	Log   string `json:"log,omitempty" yaml:"log,omitempty"`
}

func (i *Instance) Info(ctx context.Context) (ServiceInfo, error) {
	status, err := i.runtime.Status(ctx, i.State)
	if err != nil {
		status = state.StatusUnknown
	}
	info := ServiceInfo{
		Name: i.Name, Runtime: i.Runtime, Action: i.Action, Status: status,
		Version: i.State.Version, Ports: i.State.Ports, PID: i.State.PID,
		ContainerID: i.State.ContainerID, HelmRelease: i.State.HelmRelease,
		Namespace: i.State.Namespace, Connection: i.Connection,
		Paths: ServicePaths{
			State: filepath.Join(i.opts.StateDir, i.Name, "state.yaml"),
			Run:   filepath.Join(i.opts.StateDir, i.Name, "run"),
			Data:  filepath.Join(i.opts.StateDir, i.Name, "data"),
			Log:   i.State.LogFile,
		},
	}
	effective := i.State.EffectiveConfig
	if provider, ok := i.runtime.(runtimeConfigProvider); ok {
		_, spec, found := config.GetService(i.Name)
		if found {
			opts := Options{StateDir: i.opts.StateDir, Namespace: i.State.Namespace}
			if saved := i.State.StartOptions; saved != nil {
				opts.Version, opts.Port, opts.BindAddress, opts.DataDir = saved.Version, saved.Port, saved.Bind, saved.DataDir
				opts.VolumeMode, opts.Parameters = VolumeMode(saved.VolumeMode), cloneStrings(saved.Parameters)
			}
			svc := &ServiceContext{Name: i.Name, Spec: *spec, Opts: opts, DataDir: info.Paths.Data}
			live, err := provider.InspectConfig(ctx, svc, i.State)
			if err != nil {
				return ServiceInfo{}, err
			}
			if live != nil {
				effective = live
			}
		}
	}
	if effective != nil {
		info.Image, info.Chart = effective.Image, effective.Chart
		info.Parameters, info.Volume = effective.Parameters, effective.Volume
		if effective.Volume != nil && effective.Volume.Mode == string(VolumeHost) && effective.Volume.Source != "" {
			info.Paths.Data = effective.Volume.Source
		}
	}
	return info, nil
}

// ErrRestartUnsupported is returned when a runtime has no in-place restart
// (helm, command); callers should stop and start instead.
var ErrRestartUnsupported = errors.New("in-place restart not supported for this runtime")

// restarter is implemented by runtimes that can restart the service in place
// (binary via SupervisedProcess, docker via the restart API).
type restarter interface {
	Restart(ctx context.Context, stateDir string, st *state.State) error
}

// watcher is implemented by runtimes that can observe a live service for
// readiness: liveness, output, detected ports and exec probes. Start and
// Restart both build their processWatch through it so they gate identically.
type watcher interface {
	Watch(ctx context.Context, svc *ServiceContext, st *state.State, since logOffset) *processWatch
}

// Restart restarts the service in place, preserving its supervisor,
// container and connection, and blocks until it passes its readiness probes
// again. A runtime restart only proves the process or container came back —
// the service is not ready until it accepts connections.
func (i *Instance) Restart(ctx context.Context) error {
	r, ok := i.runtime.(restarter)
	if !ok {
		return ErrRestartUnsupported
	}
	// record where the log ends before restarting, so a stdout probe cannot
	// match the output of the run being replaced
	since := logOffsetOf(i.State.LogFile)
	if err := r.Restart(ctx, i.opts.StateDir, i.State); err != nil {
		return err
	}
	if err := i.awaitReady(ctx, since); err != nil {
		return fmt.Errorf("%s restarted but did not become ready: %w", i.Name, err)
	}
	i.State.Ready = true
	return i.State.Save(i.opts.StateDir)
}

// awaitReady runs the service's readiness probes against the live service,
// rebuilding its context from the options it was started with.
func (i *Instance) awaitReady(ctx context.Context, since logOffset) error {
	pkg, spec, ok := config.GetService(i.Name)
	if !ok {
		return fmt.Errorf("unknown service %q", i.Name)
	}
	options := i.opts
	if saved := i.State.StartOptions; saved != nil {
		options.Version, options.Port, options.BindAddress, options.DataDir = saved.Version, saved.Port, saved.Bind, saved.DataDir
		options.VolumeMode, options.Parameters = VolumeMode(saved.VolumeMode), cloneStrings(saved.Parameters)
		if saved.Namespace != "" {
			options.Namespace = saved.Namespace
		}
	}
	svc, err := newServiceContext(i.Name, pkg, *spec, options)
	if err != nil {
		return err
	}
	var watch *processWatch
	if w, ok := i.runtime.(watcher); ok {
		watch = w.Watch(ctx, svc, i.State, since)
	}
	return awaitHealthy(ctx, svc, runtimeHealth(*spec, i.Runtime), watch)
}

// Waiter is implemented by runtimes whose service lives in this process
// (the binary runtime); Wait blocks until the service exits.
type Waiter interface {
	Wait(ctx context.Context) error
}

// Stop stops the underlying service.
func (i *Instance) Stop(ctx context.Context) error {
	return i.runtime.Stop(ctx, i.State)
}

// Wait blocks until an in-process service exits or ctx is cancelled;
// it returns immediately for detached runtimes (docker, helm).
func (i *Instance) Wait(ctx context.Context) error {
	if w, ok := i.runtime.(Waiter); ok {
		return w.Wait(ctx)
	}
	return nil
}

// metricsProvider is implemented by runtimes that can sample live resource
// usage on demand (docker); others fall back to the persisted sample.
type metricsProvider interface {
	Metrics(ctx context.Context, st *state.State) (*state.Resources, error)
}

// Metrics returns the latest resource sample for the service: live for
// runtimes that support on-demand sampling, otherwise the sample the
// supervisor last persisted. Returns nil when nothing is available.
func (i *Instance) Metrics(ctx context.Context) *state.Resources {
	if m, ok := i.runtime.(metricsProvider); ok {
		if res, err := m.Metrics(ctx, i.State); err == nil && res != nil {
			return res
		}
	}
	return i.State.Resources
}

// logsProvider is implemented by runtimes that stream logs themselves
// (docker); others log to the service's log file.
type logsProvider interface {
	Logs(ctx context.Context, st *state.State, follow bool, w io.Writer) error
}

// Logs writes the service's logs to w: container logs for the docker
// runtime, the service log file otherwise. With follow it blocks until ctx
// is cancelled.
func (i *Instance) Logs(ctx context.Context, follow bool, w io.Writer) error {
	if lp, ok := i.runtime.(logsProvider); ok {
		return lp.Logs(ctx, i.State, follow, w)
	}
	if i.State.LogFile == "" {
		return fmt.Errorf("service %s (%s runtime) has no logs", i.Name, i.Runtime)
	}
	return tailLogFile(ctx, i.State.LogFile, follow, w)
}

// tailLogFile copies a log file to w, polling for appended output in follow
// mode until ctx is cancelled.
func tailLogFile(ctx context.Context, path string, follow bool, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(w, f); err != nil {
		return err
	}
	for follow {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
		if _, err := io.Copy(w, f); err != nil {
			return err
		}
	}
	return nil
}
