package start

import (
	"context"
	"fmt"

	"github.com/flanksource/deps/start/state"
)

// binaryRuntime installs the service artifact via deps and runs it under a
// clicky supervised process.
type binaryRuntime struct{}

func (r *binaryRuntime) Kind() RuntimeKind { return RuntimeBinary }

func (r *binaryRuntime) Start(ctx context.Context, svc *ServiceContext) (*state.State, error) {
	return nil, fmt.Errorf("binary runtime not implemented yet")
}

func (r *binaryRuntime) Stop(ctx context.Context, st *state.State) error {
	return fmt.Errorf("binary runtime not implemented yet")
}

func (r *binaryRuntime) Status(ctx context.Context, st *state.State) (state.Status, error) {
	return state.StatusUnknown, fmt.Errorf("binary runtime not implemented yet")
}
