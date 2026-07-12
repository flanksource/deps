// Package start launches services from the deps registry (postgres,
// opensearch, valkey, ...) via binary, docker or helm runtimes and emits a
// commons-db connection for each started service.
package start

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/start/state"
)

// Start launches a service and blocks until it is ready, returning the
// instance with its connection. Idempotent: an already-running service is
// reused.
func Start(ctx context.Context, name string, opts ...Option) (*Instance, error) {
	options, err := ResolveOptions(opts)
	if err != nil {
		return nil, err
	}

	pkg, spec, ok := config.GetService(name)
	if !ok {
		return nil, fmt.Errorf("unknown service %q, available: %s", name, strings.Join(ServiceNames(), ", "))
	}
	kind, err := selectRuntime(*spec, options.Runtime, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, fmt.Errorf("service %s: %w", name, err)
	}

	unlock, err := state.Lock(options.StateDir, name)
	if err != nil {
		return nil, err
	}
	defer unlock()

	svc, err := newServiceContext(name, pkg, *spec, options)
	if err != nil {
		return nil, err
	}

	rt, err := newRuntime(kind)
	if err != nil {
		return nil, err
	}
	st, err := rt.Start(ctx, svc)
	if err != nil {
		return nil, fmt.Errorf("failed to start %s (%s): %w", name, kind, err)
	}

	st.Name = name
	st.Runtime = string(kind)
	st.LogFile = svc.LogFile
	if svc.Version != "" {
		st.Version = svc.Version
	}
	if st.StartedAt.IsZero() {
		st.StartedAt = time.Now()
	}
	// A runtime that reused an already-running service returns its persisted
	// state; keep that connection (it reflects the ports it started with).
	if st.Connection.URL == "" {
		conn, err := BuildConnection(svc, st, kind)
		if err != nil {
			return nil, err
		}
		st.Connection = conn
	}
	st.Ready = true
	st.StartOptions = &state.StartOptions{
		Runtime:   string(options.Runtime),
		Version:   options.Version,
		Port:      options.Port,
		Bind:      options.BindAddress,
		Namespace: options.Namespace,
		DataDir:   options.DataDir,
	}
	if err := st.Save(options.StateDir); err != nil {
		return nil, err
	}

	return &Instance{Name: name, Runtime: kind, Connection: st.Connection, State: st, runtime: rt, stateDir: options.StateDir}, nil
}

// Get returns a previously started service, or os.IsNotExist error.
func Get(ctx context.Context, name string, opts ...Option) (*Instance, error) {
	options, err := ResolveOptions(opts)
	if err != nil {
		return nil, err
	}
	st, err := state.Load(options.StateDir, name)
	if err != nil {
		return nil, err
	}
	rt, err := newRuntime(RuntimeKind(st.Runtime))
	if err != nil {
		return nil, err
	}
	return &Instance{Name: name, Runtime: RuntimeKind(st.Runtime), Connection: st.Connection, State: st, runtime: rt, stateDir: options.StateDir}, nil
}

// Stop stops a previously started service and marks its state not-ready.
func Stop(ctx context.Context, name string, opts ...Option) error {
	options, err := ResolveOptions(opts)
	if err != nil {
		return err
	}
	instance, err := Get(ctx, name, opts...)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service %s has not been started", name)
		}
		return err
	}
	if err := instance.Stop(ctx); err != nil {
		return err
	}
	instance.State.Ready = false
	return instance.State.Save(options.StateDir)
}

// ErrNotRunning is returned by Restart when the service has no live process
// or container to restart in place.
var ErrNotRunning = errors.New("service is not running")

// Restart restarts a running service in place: a supervised binary via
// SupervisedProcess.Restart (signalling its supervisor when detached), a
// container via the docker restart API. Runtimes without in-place restart
// return ErrRestartUnsupported; a stopped service returns ErrNotRunning —
// in both cases callers should stop (if needed) and Start again.
func Restart(ctx context.Context, name string, opts ...Option) (*Instance, error) {
	options, err := ResolveOptions(opts)
	if err != nil {
		return nil, err
	}
	instance, err := Get(ctx, name, opts...)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("service %s has not been started", name)
		}
		return nil, err
	}
	if _, ok := instance.runtime.(restarter); !ok {
		return nil, ErrRestartUnsupported
	}
	if status, err := instance.runtime.Status(ctx, instance.State); err != nil || status != state.StatusRunning {
		return nil, ErrNotRunning
	}
	if err := instance.Restart(ctx); err != nil {
		return nil, err
	}
	if fresh, err := state.Load(options.StateDir, name); err == nil {
		instance.State = fresh
		instance.Connection = fresh.Connection
	}
	return instance, nil
}

// Status returns the live status of a previously started service.
func Status(ctx context.Context, name string, opts ...Option) (state.Status, error) {
	instance, err := Get(ctx, name, opts...)
	if err != nil {
		if os.IsNotExist(err) {
			return state.StatusStopped, nil
		}
		return state.StatusUnknown, err
	}
	return instance.runtime.Status(ctx, instance.State)
}

// List returns every service with persisted state.
func List(ctx context.Context, opts ...Option) ([]*Instance, error) {
	options, err := ResolveOptions(opts)
	if err != nil {
		return nil, err
	}
	states, err := state.List(options.StateDir)
	if err != nil {
		return nil, err
	}
	var instances []*Instance
	for _, st := range states {
		rt, err := newRuntime(RuntimeKind(st.Runtime))
		if err != nil {
			return nil, err
		}
		instances = append(instances, &Instance{Name: st.Name, Runtime: RuntimeKind(st.Runtime), Connection: st.Connection, State: st, runtime: rt, stateDir: options.StateDir})
	}
	return instances, nil
}

// ServiceNames lists every registry package with a service spec.
func ServiceNames() []string {
	var names []string
	for _, name := range config.ListAllPackages() {
		if _, _, ok := config.GetService(name); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func newRuntime(kind RuntimeKind) (Runtime, error) {
	switch kind {
	case RuntimeBinary:
		return &binaryRuntime{}, nil
	case RuntimeDocker:
		return &dockerRuntime{}, nil
	case RuntimeHelm:
		return &helmRuntime{}, nil
	case RuntimeCommand:
		return &commandRuntime{}, nil
	default:
		return nil, fmt.Errorf("unknown runtime %q", kind)
	}
}

// newServiceContext resolves directories and credentials for one service.
// The password is reused from prior state so restarts keep working against
// initialized data dirs.
func newServiceContext(name string, pkg types.Package, spec types.ServiceSpec, options Options) (*ServiceContext, error) {
	dir, err := state.Dir(options.StateDir, name)
	if err != nil {
		return nil, err
	}
	svc := &ServiceContext{
		Name:    name,
		Package: pkg,
		Spec:    spec,
		Opts:    options,
		Version: options.Version,
		DataDir: options.DataDir,
		RunDir:  filepath.Join(dir, "run"),
		LogFile: filepath.Join(dir, "logs", "service.log"),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
	if svc.DataDir == "" {
		svc.DataDir = filepath.Join(dir, "data")
	}
	for _, d := range []string{svc.DataDir, svc.RunDir, filepath.Dir(svc.LogFile)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	// only credentialed services get a (generated) password
	if creds := spec.Credentials; creds != nil {
		svc.Username = creds.Username
		svc.Password = creds.Password
		svc.Database = creds.Database
		if svc.Password == "" {
			svc.Password = resolvePassword(name, svc.RunDir, options.StateDir)
		}
	}
	return svc, nil
}

// resolvePassword reuses the password a data dir was initialized with: the
// run-dir password file is authoritative (written before init steps, so it
// survives failed starts), then prior state, then a fresh random password.
func resolvePassword(name, runDir, stateDir string) string {
	if data, err := os.ReadFile(filepath.Join(runDir, ".password")); err == nil && len(data) > 0 {
		return string(data)
	}
	if prior, err := state.Load(stateDir, name); err == nil && prior.Connection.Password != "" && !strings.HasPrefix(prior.Connection.Password, "secret://") {
		return prior.Connection.Password
	}
	return generatePassword()
}
