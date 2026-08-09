package start

import (
	"context"
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/start/state"
)

// restartingRuntime restarts instantly and reports liveness from alive, so
// the only thing left gating readiness is the health probe.
type restartingRuntime struct {
	restarted int
	alive     bool
}

func (r *restartingRuntime) Kind() RuntimeKind { return RuntimeDocker }
func (r *restartingRuntime) Start(context.Context, *ServiceContext) (*state.State, error) {
	return &state.State{}, nil
}
func (r *restartingRuntime) Reconcile(context.Context, *ServiceContext, *state.State) (*state.State, error) {
	return &state.State{}, nil
}
func (r *restartingRuntime) Stop(context.Context, *state.State) error { return nil }
func (r *restartingRuntime) Status(context.Context, *state.State) (state.Status, error) {
	return state.StatusRunning, nil
}
func (r *restartingRuntime) Restart(context.Context, string, *state.State) error {
	r.restarted++
	return nil
}
func (r *restartingRuntime) Watch(context.Context, *ServiceContext, *state.State) *processWatch {
	return &processWatch{alive: func() bool { return r.alive }}
}

// mssql declares a plain TCP health check on its primary port, so restart
// readiness comes down to whether that port accepts.
func restartInstance(runtime Runtime, port int) *Instance {
	stateDir := GinkgoT().TempDir()
	return &Instance{
		Name:    "mssql",
		Runtime: RuntimeDocker,
		State: &state.State{
			Name: "mssql", Runtime: "docker", Ready: true, ContainerID: "container",
			StartOptions: &state.StartOptions{Runtime: "docker", Port: port},
		},
		runtime: runtime,
		opts:    Options{StateDir: stateDir},
	}
}

var _ = Describe("health-gated restart", func() {
	It("marks a service ready only once its port accepts again", func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(listener.Close)

		runtime := &restartingRuntime{alive: true}
		instance := restartInstance(runtime, listener.Addr().(*net.TCPAddr).Port)

		Expect(instance.Restart(context.Background())).To(Succeed())
		Expect(runtime.restarted).To(Equal(1))
		Expect(instance.State.Ready).To(BeTrue())

		persisted, err := state.Load(instance.opts.StateDir, instance.Name)
		Expect(err).ToNot(HaveOccurred())
		Expect(persisted.Ready).To(BeTrue())
	})

	It("fails the restart and leaves the service not ready when it does not come back", func() {
		runtime := &restartingRuntime{alive: false}
		instance := restartInstance(runtime, freePort())

		err := instance.Restart(context.Background())
		Expect(err).To(MatchError(ContainSubstring("did not become ready")))
		Expect(runtime.restarted).To(Equal(1))
		Expect(instance.State.Ready).To(BeTrue(), "the caller's copy is untouched until the probe passes")

		_, err = state.Load(instance.opts.StateDir, instance.Name)
		Expect(err).To(MatchError(ContainSubstring("no such file")), "a failed restart persists no ready state")
	})
})
