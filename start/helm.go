package start

import (
	"context"
	"fmt"

	"github.com/flanksource/deps/start/state"
)

// helmRuntime installs the service as a helm release via the helm CLI
// (auto-installed through the deps registry).
type helmRuntime struct{}

func (r *helmRuntime) Kind() RuntimeKind { return RuntimeHelm }

func (r *helmRuntime) Start(ctx context.Context, svc *ServiceContext) (*state.State, error) {
	return nil, fmt.Errorf("helm runtime not implemented yet")
}

func (r *helmRuntime) Stop(ctx context.Context, st *state.State) error {
	return fmt.Errorf("helm runtime not implemented yet")
}

func (r *helmRuntime) Status(ctx context.Context, st *state.State) (state.Status, error) {
	return state.StatusUnknown, fmt.Errorf("helm runtime not implemented yet")
}
