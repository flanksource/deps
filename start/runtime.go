package start

import (
	"context"
	"fmt"
	"strings"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
	"github.com/flanksource/deps/start/state"
)

type RuntimeKind string

const (
	RuntimeBinary RuntimeKind = "binary"
	RuntimeDocker RuntimeKind = "docker"
	RuntimeHelm   RuntimeKind = "helm"
)

// Runtime starts and stops a service using one execution backend.
type Runtime interface {
	Kind() RuntimeKind
	// Start launches the service, waits for readiness, and returns the
	// populated state (pid/container/release + host ports).
	Start(ctx context.Context, svc *ServiceContext) (*state.State, error)
	Stop(ctx context.Context, st *state.State) error
	Status(ctx context.Context, st *state.State) (state.Status, error)
}

// ServiceContext bundles everything a runtime needs to start one service.
type ServiceContext struct {
	Name    string
	Package types.Package
	Spec    types.ServiceSpec
	Opts    Options

	// Version is the resolved artifact/image/chart app version.
	Version string
	// Username, Password and Database are the resolved credentials.
	Username string
	Password string
	Database string
	// DataDir, RunDir and LogFile live under <stateDir>/<name>/.
	DataDir string
	RunDir  string
	LogFile string
	// AppDir and BinDir point at the installed artifact (binary runtime only).
	AppDir string
	BinDir string
	// OS and Arch are the host platform.
	OS   string
	Arch string
	// Host is where the service is reachable, default "localhost". The
	// docker runtime sets it to the daemon's host when remote.
	Host string
}

// serviceHost returns the hostname services are reachable at.
func (svc *ServiceContext) serviceHost() string {
	if svc.Host != "" {
		return svc.Host
	}
	return "localhost"
}

// selectRuntime picks the runtime to use: the requested one (validated
// against the spec) or the first supported in order binary > docker > helm,
// skipping runtimes whose platform filter excludes this host.
func selectRuntime(spec types.ServiceSpec, requested RuntimeKind, os, arch string) (RuntimeKind, error) {
	supported := spec.Runtimes()
	if requested != "" {
		for _, kind := range supported {
			if kind == string(requested) {
				return requested, nil
			}
		}
		return "", fmt.Errorf("runtime %q not supported, available: %s", requested, strings.Join(supported, ", "))
	}
	if len(supported) == 0 {
		return "", fmt.Errorf("service defines no runtimes")
	}
	for _, kind := range supported {
		if checkPlatform(runtimePlatforms(spec, RuntimeKind(kind)), os, arch) == nil {
			return RuntimeKind(kind), nil
		}
	}
	return "", fmt.Errorf("no runtime supports %s-%s (available: %s)", os, arch, strings.Join(supported, ", "))
}

func runtimePlatforms(spec types.ServiceSpec, kind RuntimeKind) []string {
	switch kind {
	case RuntimeBinary:
		return spec.Binary.Platforms
	case RuntimeDocker:
		return spec.Docker.Platforms
	default:
		return nil
	}
}

// resolveServiceVersion resolves an unset version to the latest published
// release via the package's manager (so image tags like
// elasticsearch:<version>, which have no "latest", still work). Service-only
// entries without an installable artifact fall back to the "latest" tag.
func resolveServiceVersion(ctx context.Context, svc *ServiceContext) (string, error) {
	if svc.Package.Manager == "" {
		return "latest", nil
	}
	mgr, ok := manager.GetGlobalRegistry().Get(svc.Package.Manager)
	if !ok {
		return "latest", nil
	}
	resolved, err := version.NewResolver(mgr).ResolveConstraint(ctx, svc.Package, "latest", platform.Current())
	if err != nil {
		return "", fmt.Errorf("failed to resolve latest version of %s: %w", svc.Name, err)
	}
	return resolved, nil
}
