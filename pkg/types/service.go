package types

// ServiceSpec describes how to run a package as a long-lived service and how
// to emit a connection for it. Runtime support is implied by which of
// Binary/Docker/Helm are non-nil. Consumed by github.com/flanksource/deps/start.
//
// String fields support templating with: {{.name}} {{.version}} {{.tag}}
// {{.major}} {{.minor}} {{.os}} {{.arch}} {{.port}} {{.ports.<name>}} {{.host}}
// {{.appDir}} {{.binDir}} {{.dataDir}} {{.runDir}} {{.passwordFile}}
// {{.username}} {{.password}} {{.database}} {{.release}} {{.namespace}}, and
// service-specific values through {{index .parameters "flag-name"}}.
type ServiceSpec struct {
	// Type is the connection type string (commons-db ConnectionType: postgres,
	// mysql, sql_server, clickhouse, redis, opensearch, elasticsearch, loki,
	// prometheus, http, aws). Free-form strings are allowed (nats, rabbitmq).
	Type string `json:"type" yaml:"type"`
	// Ports are the named listen ports. The first is primary unless Primary is set.
	// Names must be identifier-safe (no dashes) for {{.ports.<name>}} templating.
	Ports []ServicePort `json:"ports,omitempty" yaml:"ports,omitempty"`
	// Credentials are templatable defaults; an empty Password means a random
	// one is generated at start.
	Credentials *ServiceCredentials `json:"credentials,omitempty" yaml:"credentials,omitempty"`
	// URL is the connection URL template. {{.host}} resolves per runtime:
	// binary/docker -> "localhost:<mapped primary port>", helm -> "svc://<service>.<namespace>:<port>".
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
	// Properties are extra templated key/values copied to the emitted connection.
	Properties map[string]string `json:"properties,omitempty" yaml:"properties,omitempty"`
	// Health is the default readiness check; runtime blocks may override it.
	Health *HealthCheck `json:"health,omitempty" yaml:"health,omitempty"`
	// InsecureTLS marks the emitted connection as skipping TLS verification
	// (self-signed local API servers).
	InsecureTLS bool `json:"insecure_tls,omitempty" yaml:"insecure_tls,omitempty"`
	// Parameters become typed, service-specific deps-start flags. Map keys are
	// kebab-case Cobra flag names and are available to templates through
	// {{index .parameters "flag-name"}}.
	Parameters map[string]ServiceParameter `json:"parameters,omitempty" yaml:"parameters,omitempty"`

	Binary  *BinaryRuntime  `json:"binary,omitempty" yaml:"binary,omitempty"`
	Docker  *DockerRuntime  `json:"docker,omitempty" yaml:"docker,omitempty"`
	Helm    *HelmRuntime    `json:"helm,omitempty" yaml:"helm,omitempty"`
	Command *CommandRuntime `json:"command,omitempty" yaml:"command,omitempty"`
}

type ServiceParameterType string

const (
	ServiceParameterString   ServiceParameterType = "string"
	ServiceParameterBool     ServiceParameterType = "bool"
	ServiceParameterInt      ServiceParameterType = "int"
	ServiceParameterDuration ServiceParameterType = "duration"
	ServiceParameterQuantity ServiceParameterType = "quantity"
)

// ServiceParameter describes one generated deps-start flag.
type ServiceParameter struct {
	Type        ServiceParameterType `json:"type" yaml:"type"`
	Description string               `json:"description" yaml:"description"`
	Default     string               `json:"default,omitempty" yaml:"default,omitempty"`
	Runtimes    []string             `json:"runtimes,omitempty" yaml:"runtimes,omitempty"`
	Pattern     string               `json:"pattern,omitempty" yaml:"pattern,omitempty"`
}

// ServicePort is a named service port.
type ServicePort struct {
	Name    string `json:"name" yaml:"name"`
	Port    int    `json:"port" yaml:"port"`
	Primary bool   `json:"primary,omitempty" yaml:"primary,omitempty"`
	// Protocol is tcp (default) or udp.
	Protocol string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}

// ServiceCredentials are templated default credentials.
type ServiceCredentials struct {
	Username string `json:"username,omitempty" yaml:"username,omitempty"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	Database string `json:"database,omitempty" yaml:"database,omitempty"`
}

// HealthCheck defines service readiness. Set exactly one probe of
// HTTP/StdoutMatch/Exec, or none for a plain TCP wait on Port.
type HealthCheck struct {
	// Port is the named port to probe (default: the primary port).
	Port string `json:"port,omitempty" yaml:"port,omitempty"`
	// HTTP is a path probed with GET expecting a 2xx response.
	HTTP string `json:"http,omitempty" yaml:"http,omitempty"`
	// StdoutMatch is a substring awaited on stdout/stderr (binary runtime only).
	StdoutMatch string `json:"stdout_match,omitempty" yaml:"stdout_match,omitempty"`
	// Exec is a command probe (docker HEALTHCHECK or local exec).
	Exec []string `json:"exec,omitempty" yaml:"exec,omitempty"`
	// Timeout is a duration string, default "120s".
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	// Interval is a duration string, default "2s".
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
}

// BinaryRuntime launches the installed artifact as a supervised process.
type BinaryRuntime struct {
	// Package is the registry entry providing the artifact (default: the
	// service's own registry key).
	Package string `json:"package,omitempty" yaml:"package,omitempty"`
	// Requires are extra registry packages installed before start (e.g.
	// kube-apiserver requires etcd).
	Requires []string `json:"requires,omitempty" yaml:"requires,omitempty"`
	// Command is the executable: relative paths resolve against the installed
	// app dir (directory mode); templates may use {{.binDir}} for binary-mode installs.
	Command string            `json:"command" yaml:"command"`
	Args    []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	// Files maps runDir-relative filenames to templated content, rendered
	// before start (config generation for prometheus/loki/otel).
	Files map[string]string `json:"files,omitempty" yaml:"files,omitempty"`
	// Init are one-time setup commands (e.g. initdb), skipped when Creates exists.
	Init []InitStep `json:"init,omitempty" yaml:"init,omitempty"`
	// Platforms restricts the binary runtime (e.g. ["linux-*"]); empty = all.
	Platforms []string     `json:"platforms,omitempty" yaml:"platforms,omitempty"`
	Health    *HealthCheck `json:"health,omitempty" yaml:"health,omitempty"`
}

// InitStep is a one-time setup command run before first service start.
type InitStep struct {
	// Command is a templated shell command; cwd is the installed app dir.
	Command string `json:"command" yaml:"command"`
	// Creates skips the step when this templated path exists.
	Creates string `json:"creates,omitempty" yaml:"creates,omitempty"`
}

// DockerRuntime runs the service as a container.
type DockerRuntime struct {
	// Image is a templated reference, e.g. "postgres:{{.major}}".
	Image string `json:"image" yaml:"image"`
	// DefaultVersion is used when no version is requested, instead of
	// resolving the latest artifact release (image registries can lag the
	// artifact archive, e.g. apache/activemq-classic).
	DefaultVersion string `json:"default_version,omitempty" yaml:"default_version,omitempty"`
	// Command overrides the container entrypoint arguments.
	Command []string `json:"command,omitempty" yaml:"command,omitempty"`
	// Args are appended CMD arguments.
	Args []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	// DataPath is the container path bound to {{.dataDir}}.
	DataPath string `json:"data_path,omitempty" yaml:"data_path,omitempty"`
	// Volumes are extra "hostTemplate:containerPath" mounts.
	Volumes []string `json:"volumes,omitempty" yaml:"volumes,omitempty"`
	// Files maps container paths to templated content (mounted read-only).
	Files     map[string]string `json:"files,omitempty" yaml:"files,omitempty"`
	Platforms []string          `json:"platforms,omitempty" yaml:"platforms,omitempty"`
	// Privileged runs the container in privileged mode (k3s).
	Privileged bool         `json:"privileged,omitempty" yaml:"privileged,omitempty"`
	Health     *HealthCheck `json:"health,omitempty" yaml:"health,omitempty"`
}

// CommandRuntime manages services whose lifecycle is driven by a CLI that
// runs to completion (e.g. kind create/delete cluster). The CLI binary is
// installed from the registry.
type CommandRuntime struct {
	// Package is the registry entry providing the CLI (default: the
	// service's own registry key).
	Package string `json:"package,omitempty" yaml:"package,omitempty"`
	// Start/Stop are templated commands run to completion.
	Start string `json:"start" yaml:"start"`
	Stop  string `json:"stop" yaml:"stop"`
	// Status is a templated command whose zero exit code means running.
	Status string `json:"status,omitempty" yaml:"status,omitempty"`
	// Files maps runDir-relative filenames to templated content rendered
	// before start (e.g. a kind cluster config).
	Files     map[string]string `json:"files,omitempty" yaml:"files,omitempty"`
	Env       map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Platforms []string          `json:"platforms,omitempty" yaml:"platforms,omitempty"`
	Health    *HealthCheck      `json:"health,omitempty" yaml:"health,omitempty"`
}

// HelmRuntime installs the service as a helm release.
type HelmRuntime struct {
	// Chart is either an oci:// reference or a chart name used with Repo.
	Chart string `json:"chart" yaml:"chart"`
	// Repo is the https chart repository URL when Chart is not oci://.
	Repo string `json:"repo,omitempty" yaml:"repo,omitempty"`
	// ChartVersion pins the chart (not the app) version.
	ChartVersion string `json:"chart_version,omitempty" yaml:"chart_version,omitempty"`
	// ReleaseName is a template, default "deps-{{.name}}".
	ReleaseName string `json:"release_name,omitempty" yaml:"release_name,omitempty"`
	// Values is inline templated YAML passed via -f.
	Values string `json:"values,omitempty" yaml:"values,omitempty"`
	// Set are templated --set pairs.
	Set map[string]string `json:"set,omitempty" yaml:"set,omitempty"`
	// PortSet contains chart values applied only when --port is supplied.
	// Values are templates and override matching Set entries.
	PortSet map[string]string `json:"port_set,omitempty" yaml:"port_set,omitempty"`
	// ResourcePrefix is the chart values path for the primary workload's
	// Kubernetes resource requirements, for example "server.resources".
	ResourcePrefix string `json:"resource_prefix,omitempty" yaml:"resource_prefix,omitempty"`
	// Volume maps the service's primary data mount to chart-specific values.
	Volume *HelmVolume `json:"volume,omitempty" yaml:"volume,omitempty"`
	// Secret identifies the chart-created credential secret so the emitted
	// connection password can use "secret://<name>/<key>".
	Secret *SecretRef `json:"secret,omitempty" yaml:"secret,omitempty"`
	// Service is the in-cluster endpoint used to build "svc://<name>.<namespace>:<port>".
	Service *ServiceRef  `json:"service,omitempty" yaml:"service,omitempty"`
	Health  *HealthCheck `json:"health,omitempty" yaml:"health,omitempty"`
}

type HelmVolume struct {
	MountPath string                    `json:"mount_path" yaml:"mount_path"`
	Modes     map[string]HelmVolumeMode `json:"modes" yaml:"modes"`
}

type HelmVolumeMode struct {
	Set map[string]string `json:"set,omitempty" yaml:"set,omitempty"`
}

// SecretRef points at a key in a chart-created Kubernetes secret.
type SecretRef struct {
	// Name is templated, e.g. "{{.release}}-postgres".
	Name string `json:"name" yaml:"name"`
	// Key is the password key within the secret.
	Key string `json:"key" yaml:"key"`
	// UsernameKey optionally names the username key within the secret.
	UsernameKey string `json:"username_key,omitempty" yaml:"username_key,omitempty"`
}

// ServiceRef is the templated Kubernetes Service exposing the release.
type ServiceRef struct {
	Name string `json:"name" yaml:"name"`
	// Port defaults to the primary service port.
	Port int `json:"port,omitempty" yaml:"port,omitempty"`
}

// PrimaryPort returns the port marked Primary, or the first port.
func (s ServiceSpec) PrimaryPort() (ServicePort, bool) {
	for _, p := range s.Ports {
		if p.Primary {
			return p, true
		}
	}
	if len(s.Ports) > 0 {
		return s.Ports[0], true
	}
	return ServicePort{}, false
}

// Runtimes returns the names of the runtimes this service supports.
func (s ServiceSpec) Runtimes() []string {
	var kinds []string
	if s.Binary != nil {
		kinds = append(kinds, "binary")
	}
	if s.Docker != nil {
		kinds = append(kinds, "docker")
	}
	if s.Helm != nil {
		kinds = append(kinds, "helm")
	}
	if s.Command != nil {
		kinds = append(kinds, "command")
	}
	return kinds
}
