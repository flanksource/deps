package start

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/deps/start/state"
)

// Instance is a started (or previously started) service.
type Instance struct {
	Name       string            `json:"name" yaml:"name"`
	Runtime    RuntimeKind       `json:"runtime" yaml:"runtime"`
	Connection models.Connection `json:"connection" yaml:"connection"`

	State    *state.State `json:"-" yaml:"-"`
	runtime  Runtime
	stateDir string
}

// ErrRestartUnsupported is returned when a runtime has no in-place restart
// (helm, command); callers should stop and start instead.
var ErrRestartUnsupported = errors.New("in-place restart not supported for this runtime")

// restarter is implemented by runtimes that can restart the service in place
// (binary via SupervisedProcess, docker via the restart API).
type restarter interface {
	Restart(ctx context.Context, stateDir string, st *state.State) error
}

// Restart restarts the service in place, preserving its supervisor,
// container and connection.
func (i *Instance) Restart(ctx context.Context) error {
	r, ok := i.runtime.(restarter)
	if !ok {
		return ErrRestartUnsupported
	}
	return r.Restart(ctx, i.stateDir, i.State)
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
