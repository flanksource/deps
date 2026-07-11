package start

import (
	"context"
	"fmt"

	"github.com/flanksource/deps/start/state"
)

// dockerRuntime runs the service as a container via the docker SDK.
type dockerRuntime struct{}

func (r *dockerRuntime) Kind() RuntimeKind { return RuntimeDocker }

func (r *dockerRuntime) Start(ctx context.Context, svc *ServiceContext) (*state.State, error) {
	return nil, fmt.Errorf("docker runtime not implemented yet")
}

func (r *dockerRuntime) Stop(ctx context.Context, st *state.State) error {
	return fmt.Errorf("docker runtime not implemented yet")
}

func (r *dockerRuntime) Status(ctx context.Context, st *state.State) (state.Status, error) {
	return state.StatusUnknown, fmt.Errorf("docker runtime not implemented yet")
}
