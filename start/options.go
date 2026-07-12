package start

import (
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
	// Namespace for the helm runtime.
	Namespace string
	// DataDir overrides the service data directory.
	DataDir string
	// Env adds/overrides service environment variables.
	Env map[string]string
	// Detach runs the supervisor in the background (CLI only).
	Detach bool
	// StateDir is where service state is kept, default ~/.deps/services.
	StateDir string
	// WaitTimeout bounds the readiness wait, default 120s.
	WaitTimeout time.Duration
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
	for _, p := range []*string{&options.StateDir, &options.DataDir} {
		if *p == "" {
			continue
		}
		abs, err := filepath.Abs(*p)
		if err != nil {
			return options, err
		}
		*p = abs
	}
	return options, nil
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

func WithRuntime(kind RuntimeKind) Option    { return func(o *Options) { o.Runtime = kind } }
func WithVersion(version string) Option      { return func(o *Options) { o.Version = version } }
func WithPort(port int) Option               { return func(o *Options) { o.Port = port } }
func WithNamespace(ns string) Option         { return func(o *Options) { o.Namespace = ns } }
func WithDataDir(dir string) Option          { return func(o *Options) { o.DataDir = dir } }
func WithEnv(env map[string]string) Option   { return func(o *Options) { o.Env = env } }
func WithDetach(detach bool) Option          { return func(o *Options) { o.Detach = detach } }
func WithStateDir(dir string) Option         { return func(o *Options) { o.StateDir = dir } }
func WithWaitTimeout(d time.Duration) Option { return func(o *Options) { o.WaitTimeout = d } }
