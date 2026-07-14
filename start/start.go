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
	if err := validateSuppliedServiceParameters(*spec, options.Parameters); err != nil {
		return nil, fmt.Errorf("service %s: %w", name, err)
	}

	unlock, err := state.Lock(options.StateDir, name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	prior, err := state.Load(options.StateDir, name)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := mergePriorOptions(&options, prior); err != nil {
		return nil, fmt.Errorf("service %s: %w", name, err)
	}
	kind, err := selectRuntime(*spec, options.Runtime, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, fmt.Errorf("service %s: %w", name, err)
	}
	parameters, persistedParameters, err := resolveServiceParameters(*spec, kind, options.Parameters)
	if err != nil {
		return nil, fmt.Errorf("service %s: %w", name, err)
	}
	rt, err := newRuntime(kind)
	if err != nil {
		return nil, err
	}
	if prior != nil && prior.Runtime != string(kind) {
		if err := stopPreviousRuntime(ctx, prior); err != nil {
			return nil, err
		}
		prior = nil
	}

	svc, err := newServiceContext(name, pkg, *spec, options)
	if err != nil {
		return nil, err
	}
	svc.Parameters = parameters
	svc.Opts.Parameters = persistedParameters

	var liveConfig, desiredConfig *state.EffectiveConfig
	if provider, ok := rt.(runtimeConfigProvider); ok {
		if prior != nil {
			liveConfig, err = provider.InspectConfig(ctx, svc, prior)
			if err != nil {
				return nil, err
			}
		}
		if svc.Opts.VolumeMode == "" {
			svc.Opts.VolumeMode = defaultVolumeMode(kind, svc, liveConfig)
		}
		desiredConfig, err = provider.DesiredConfig(ctx, svc)
		if err != nil {
			return nil, err
		}
	} else {
		if options.VolumeMode != "" {
			return nil, fmt.Errorf("volume mode is only supported by docker and helm runtimes")
		}
		desiredConfig = baseEffectiveConfig(svc, kind)
	}

	desiredOptions := state.StartOptions{
		Runtime: string(kind), Version: options.Version, Port: options.Port,
		Bind: options.BindAddress, Namespace: options.Namespace, DataDir: options.DataDir,
		VolumeMode: string(svc.Opts.VolumeMode), Parameters: persistedParameters,
	}
	var change *ConfigChange
	if prior != nil {
		change, err = NewConfigChange(liveConfig, desiredConfig)
		if err != nil {
			return nil, err
		}
	}
	runtimePrior := prior
	if change != nil {
		copy := *prior
		copy.StartOptions = nil
		runtimePrior = &copy
	}
	action := runtimeAction(ctx, rt, prior, change)

	st, err := startRuntime(ctx, rt, svc, runtimePrior, desiredOptions)
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
	st.StartOptions = &desiredOptions
	st.EffectiveConfig = desiredConfig
	if err := st.Save(options.StateDir); err != nil {
		return nil, err
	}

	return &Instance{Name: name, Runtime: kind, Connection: st.Connection, Action: action, Change: change, State: st, runtime: rt, stateDir: options.StateDir}, nil
}

func mergePriorOptions(options *Options, prior *state.State) error {
	if prior == nil || prior.StartOptions == nil {
		return nil
	}
	if options.supplied.runtime && options.Runtime != RuntimeKind(prior.Runtime) {
		return nil
	}
	saved := prior.StartOptions
	if !options.supplied.runtime {
		options.Runtime = RuntimeKind(prior.Runtime)
	}
	if !options.supplied.version {
		options.Version = saved.Version
	}
	if !options.supplied.port {
		options.Port = saved.Port
	}
	if !options.supplied.bind {
		options.BindAddress = saved.Bind
	}
	if !options.supplied.namespace && saved.Namespace != "" {
		options.Namespace = saved.Namespace
	}
	if !options.supplied.dataDir && !(options.supplied.volumeMode && options.VolumeMode != VolumeHost) {
		options.DataDir = saved.DataDir
	}
	if !options.supplied.volumeMode && !options.supplied.dataDir {
		options.VolumeMode = VolumeMode(saved.VolumeMode)
	}
	parameters := cloneStrings(saved.Parameters)
	for name, value := range options.Parameters {
		parameters[name] = value
	}
	options.Parameters = parameters
	return validateOptions(options)
}

func defaultVolumeMode(kind RuntimeKind, svc *ServiceContext, live *state.EffectiveConfig) VolumeMode {
	if svc.Opts.DataDir != "" {
		return VolumeHost
	}
	if live != nil && live.Volume != nil {
		return VolumeMode(live.Volume.Mode)
	}
	if kind == RuntimeDocker && svc.Spec.Docker != nil && svc.Spec.Docker.DataPath != "" {
		return VolumeHost
	}
	if kind == RuntimeHelm && svc.Spec.Helm != nil && svc.Spec.Helm.Volume != nil {
		return VolumePersistent
	}
	return ""
}

func baseEffectiveConfig(svc *ServiceContext, kind RuntimeKind) *state.EffectiveConfig {
	return &state.EffectiveConfig{
		Runtime: string(kind), Version: svc.Version, Parameters: cloneStrings(svc.Opts.Parameters),
		Ports: configuredPorts(svc), Bind: bindAddress(svc), Namespace: svc.Opts.Namespace,
	}
}

func runtimeAction(ctx context.Context, runtime Runtime, prior *state.State, change *ConfigChange) string {
	if prior == nil {
		return "created"
	}
	if change != nil {
		return "updated"
	}
	status, err := runtime.Status(ctx, prior)
	if err == nil && status == state.StatusRunning && prior.Ready {
		return "reused"
	}
	return "started"
}

func startRuntime(ctx context.Context, runtime Runtime, svc *ServiceContext, prior *state.State, desired state.StartOptions) (*state.State, error) {
	if prior == nil {
		return runtime.Start(ctx, svc)
	}
	if prior.Runtime != string(runtime.Kind()) {
		return nil, fmt.Errorf("cannot reconcile %s state with %s runtime", prior.Runtime, runtime.Kind())
	}
	priorOptions := prior.StartOptions
	if priorOptions != nil && priorOptions.Runtime == "" {
		normalized := *priorOptions
		normalized.Runtime = prior.Runtime
		priorOptions = &normalized
	}
	if prior.Ready && priorOptions.Equal(&desired) {
		status, err := runtime.Status(ctx, prior)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect existing %s service: %w", runtime.Kind(), err)
		}
		if status == state.StatusRunning {
			return prior, nil
		}
		return runtime.Start(ctx, svc)
	}
	return runtime.Reconcile(ctx, svc, prior)
}

func stopPreviousRuntime(ctx context.Context, prior *state.State) error {
	runtime, err := newRuntime(RuntimeKind(prior.Runtime))
	if err != nil {
		return fmt.Errorf("failed to load previous runtime for %s: %w", prior.Name, err)
	}
	status, err := runtime.Status(ctx, prior)
	if err != nil {
		return fmt.Errorf("failed to inspect previous %s runtime for %s: %w", prior.Runtime, prior.Name, err)
	}
	if status == state.StatusStopped {
		return nil
	}
	if err := runtime.Stop(ctx, prior); err != nil {
		return fmt.Errorf("failed to stop previous %s runtime for %s: %w", prior.Runtime, prior.Name, err)
	}
	return nil
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
			passwordFile := filepath.Join(svc.RunDir, ".password")
			if err := os.WriteFile(passwordFile, []byte(svc.Password), 0o600); err != nil {
				return nil, fmt.Errorf("failed to persist password for %s: %w", name, err)
			}
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
