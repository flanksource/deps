package start

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type Options struct {
	// Runtime forces a specific runtime; empty selects the first supported
	// one in order binary > docker > helm.
	Runtime RuntimeKind
	// Version of the package/image/chart app; empty means latest/spec default.
	Version string
	// Port overrides the host port for the primary service port.
	Port int
	// BindAddress is the address services listen on, default 127.0.0.1.
	// Use 0.0.0.0 to expose services on all interfaces.
	BindAddress string
	// Namespace for the helm runtime.
	Namespace string
	// DataDir overrides the service data directory.
	DataDir string
	// VolumeMode controls the primary service data volume for docker and helm.
	VolumeMode VolumeMode
	// Env adds/overrides service environment variables.
	Env map[string]string
	// Parameters are explicit service-specific flag values keyed by registry
	// parameter name. Start validates and resolves them for the chosen runtime.
	Parameters map[string]string
	// Update resolves the version constraint and installs the newest match.
	// Without it an already-installed artifact is used as-is, so starting a
	// service performs no version resolution or download.
	Update bool
	// StateDir is where service state is kept, default ~/.deps/services.
	StateDir string
	// WaitTimeout bounds the readiness wait, default 120s.
	WaitTimeout time.Duration
	// LogWriter receives a copy of the service's stdout/stderr while it is
	// starting, so callers can show progress. The service log file is always
	// written regardless.
	LogWriter io.Writer
	// OnWaiting is called on every probe interval with the unmet readiness
	// condition, so callers can report what the wait is blocked on.
	OnWaiting func(Readiness)

	supplied optionPresence
}

type optionPresence struct {
	runtime, version, port, bind, namespace, dataDir, volumeMode, parameters bool
}

type Option func(*Options)

// ResolveOptions applies opts over the defaults and absolutizes paths: init
// steps and the supervised process run with their own working directories,
// so every path handed to them must be absolute.
func ResolveOptions(opts []Option) (Options, error) {
	options := DefaultOptions()
	for _, o := range opts {
		o(&options)
	}
	if err := validateOptions(&options); err != nil {
		return options, err
	}
	return options, nil
}

func validateOptions(options *Options) error {
	if options.Port < 0 || options.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", options.Port)
	}
	if options.VolumeMode != "" && !options.VolumeMode.Valid() {
		return fmt.Errorf("volume mode must be persistent, host or ephemeral, got %q", options.VolumeMode)
	}
	if options.DataDir != "" && options.VolumeMode != "" && options.VolumeMode != VolumeHost {
		return fmt.Errorf("data-dir can only be used with host volume mode")
	}
	for _, p := range []*string{&options.StateDir, &options.DataDir} {
		if *p == "" {
			continue
		}
		abs, err := filepath.Abs(*p)
		if err != nil {
			return err
		}
		*p = abs
	}
	return nil
}

func DefaultOptions() Options {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return Options{
		Namespace:   "default",
		StateDir:    filepath.Join(home, ".deps", "services"),
		WaitTimeout: 120 * time.Second,
	}
}

func WithRuntime(kind RuntimeKind) Option {
	return func(o *Options) { o.Runtime, o.supplied.runtime = kind, true }
}
func WithVersion(version string) Option {
	return func(o *Options) { o.Version, o.supplied.version = version, true }
}
func WithPort(port int) Option {
	return func(o *Options) { o.Port, o.supplied.port = port, true }
}
func WithBindAddress(addr string) Option {
	return func(o *Options) { o.BindAddress, o.supplied.bind = addr, true }
}
func WithNamespace(ns string) Option {
	return func(o *Options) { o.Namespace, o.supplied.namespace = ns, true }
}
func WithDataDir(dir string) Option {
	return func(o *Options) { o.DataDir, o.supplied.dataDir = dir, true }
}
func WithVolumeMode(mode VolumeMode) Option {
	return func(o *Options) { o.VolumeMode, o.supplied.volumeMode = mode, true }
}
func WithEnv(env map[string]string) Option { return func(o *Options) { o.Env = env } }
func WithParameters(parameters map[string]string) Option {
	return func(o *Options) {
		o.supplied.parameters = true
		o.Parameters = make(map[string]string, len(parameters))
		for name, value := range parameters {
			o.Parameters[name] = value
		}
	}
}
func WithUpdate(update bool) Option          { return func(o *Options) { o.Update = update } }
func WithStateDir(dir string) Option         { return func(o *Options) { o.StateDir = dir } }
func WithWaitTimeout(d time.Duration) Option { return func(o *Options) { o.WaitTimeout = d } }

// WithLogWriter tees the starting service's output to w in addition to its
// log file.
func WithLogWriter(w io.Writer) Option { return func(o *Options) { o.LogWriter = w } }

// WithOnWaiting reports the unmet readiness condition on every probe interval.
func WithOnWaiting(fn func(Readiness)) Option { return func(o *Options) { o.OnWaiting = fn } }
