package start

import (
	"context"
	"fmt"
	"strings"

	"github.com/flanksource/deps/pkg/types"
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
}

// selectRuntime picks the runtime to use: the requested one (validated
// against the spec) or the first supported in order binary > docker > helm.
func selectRuntime(spec types.ServiceSpec, requested RuntimeKind) (RuntimeKind, error) {
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
	return RuntimeKind(supported[0]), nil
}
