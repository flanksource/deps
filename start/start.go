// Package start launches services from the deps registry (postgres,
// opensearch, valkey, ...) via binary, docker or helm runtimes and emits a
// commons-db connection for each started service.
package start

import (
	"context"
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
	options := DefaultOptions()
	for _, o := range opts {
		o(&options)
	}

	pkg, spec, ok := config.GetService(name)
	if !ok {
		return nil, fmt.Errorf("unknown service %q, available: %s", name, strings.Join(ServiceNames(), ", "))
	}
	kind, err := selectRuntime(*spec, options.Runtime)
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
	st.Version = svc.Version
	st.LogFile = svc.LogFile
	if st.StartedAt.IsZero() {
		st.StartedAt = time.Now()
	}
	conn, err := BuildConnection(svc, st, kind)
	if err != nil {
		return nil, err
	}
	st.Connection = conn
	st.Ready = true
	if err := st.Save(options.StateDir); err != nil {
		return nil, err
	}

	return &Instance{Name: name, Runtime: kind, Connection: conn, State: st, runtime: rt}, nil
}

// Get returns a previously started service, or os.IsNotExist error.
func Get(ctx context.Context, name string, opts ...Option) (*Instance, error) {
	options := DefaultOptions()
	for _, o := range opts {
		o(&options)
	}
	st, err := state.Load(options.StateDir, name)
	if err != nil {
		return nil, err
	}
	rt, err := newRuntime(RuntimeKind(st.Runtime))
	if err != nil {
		return nil, err
	}
	return &Instance{Name: name, Runtime: RuntimeKind(st.Runtime), Connection: st.Connection, State: st, runtime: rt}, nil
}

// Stop stops a previously started service.
func Stop(ctx context.Context, name string, opts ...Option) error {
	instance, err := Get(ctx, name, opts...)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service %s has not been started", name)
		}
		return err
	}
	return instance.Stop(ctx)
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
	options := DefaultOptions()
	for _, o := range opts {
		o(&options)
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
		instances = append(instances, &Instance{Name: st.Name, Runtime: RuntimeKind(st.Runtime), Connection: st.Connection, State: st, runtime: rt})
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

	if creds := spec.Credentials; creds != nil {
		svc.Username = creds.Username
		svc.Password = creds.Password
		svc.Database = creds.Database
	}
	if svc.Password == "" {
		if prior, err := state.Load(options.StateDir, name); err == nil && prior.Connection.Password != "" && !strings.HasPrefix(prior.Connection.Password, "secret://") {
			svc.Password = prior.Connection.Password
		} else {
			svc.Password = generatePassword()
		}
	}
	return svc, nil
}
