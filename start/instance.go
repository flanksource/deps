package start

import (
	"context"

	"github.com/flanksource/commons-db/models"
	"github.com/flanksource/deps/start/state"
)

// Instance is a started (or previously started) service.
type Instance struct {
	Name       string            `json:"name" yaml:"name"`
	Runtime    RuntimeKind       `json:"runtime" yaml:"runtime"`
	Connection models.Connection `json:"connection" yaml:"connection"`

	State   *state.State `json:"-" yaml:"-"`
	runtime Runtime
}

// Waiter is implemented by runtimes whose service lives in this process
// (the binary runtime); Wait blocks until the service exits.
type Waiter interface {
	Wait(ctx context.Context) error
}

// Stop stops the underlying service.
func (i *Instance) Stop(ctx context.Context) error {
	return i.runtime.Stop(ctx, i.State)
}

// Wait blocks until an in-process service exits or ctx is cancelled;
// it returns immediately for detached runtimes (docker, helm).
func (i *Instance) Wait(ctx context.Context) error {
	if w, ok := i.runtime.(Waiter); ok {
		return w.Wait(ctx)
	}
	return nil
}
