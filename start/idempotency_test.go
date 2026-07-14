package start

import (
	"context"

	"github.com/flanksource/commons-db/models"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/start/state"
)

type recordingRuntime struct {
	status         state.Status
	started        int
	reconciled     int
	reconciledWith *state.State
}

func (r *recordingRuntime) Kind() RuntimeKind { return RuntimeDocker }
func (r *recordingRuntime) Start(context.Context, *ServiceContext) (*state.State, error) {
	r.started++
	return &state.State{Name: "nats"}, nil
}
func (r *recordingRuntime) Reconcile(_ context.Context, _ *ServiceContext, prior *state.State) (*state.State, error) {
	r.reconciled++
	r.reconciledWith = prior
	return &state.State{Name: "nats"}, nil
}
func (r *recordingRuntime) Stop(context.Context, *state.State) error { return nil }
func (r *recordingRuntime) Status(context.Context, *state.State) (state.Status, error) {
	return r.status, nil
}

var _ = Describe("idempotent starts", func() {
	desired := state.StartOptions{
		Runtime:    "docker",
		Parameters: map[string]string{"jetstream": "true"},
	}

	It("reuses a running service with identical effective options", func() {
		prior := &state.State{
			Name:         "nats",
			Runtime:      "docker",
			Ready:        true,
			StartOptions: &desired,
			Connection:   models.Connection{Password: "same-password"},
		}
		runtime := &recordingRuntime{status: state.StatusRunning}

		result, err := startRuntime(context.Background(), runtime, &ServiceContext{}, prior, desired)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeIdenticalTo(prior))
		Expect(result.Connection.Password).To(Equal("same-password"))
		Expect(runtime.started).To(BeZero())
		Expect(runtime.reconciled).To(BeZero())
	})

	It("reconciles a service when a parameter changes", func() {
		priorOptions := desired
		priorOptions.Parameters = map[string]string{"jetstream": "false"}
		prior := &state.State{Name: "nats", Runtime: "docker", StartOptions: &priorOptions}
		runtime := &recordingRuntime{status: state.StatusRunning}

		_, err := startRuntime(context.Background(), runtime, &ServiceContext{}, prior, desired)
		Expect(err).ToNot(HaveOccurred())
		Expect(runtime.reconciled).To(Equal(1))
		Expect(runtime.reconciledWith).To(BeIdenticalTo(prior))
		Expect(runtime.started).To(BeZero())
	})

	It("renders a stable canonical diff without credentials", func() {
		live := state.EffectiveConfig{
			Runtime:    "docker",
			Image:      "opensearchproject/opensearch:3.7.0",
			Parameters: map[string]string{"jvm-memory": "512m"},
		}
		desired := live
		desired.Parameters = map[string]string{"jvm-memory": "1g"}

		change, err := NewConfigChange(&live, &desired)
		Expect(err).ToNot(HaveOccurred())
		Expect(change).ToNot(BeNil())
		Expect(change.Before).To(ContainSubstring("jvm-memory: 512m"))
		Expect(change.After).To(ContainSubstring("jvm-memory: 1g"))
		Expect(change.Before).ToNot(ContainSubstring("password"))
	})

	It("starts a stopped service without reconciling unchanged options", func() {
		prior := &state.State{Name: "nats", Runtime: "docker", Ready: true, StartOptions: &desired}
		runtime := &recordingRuntime{status: state.StatusStopped}

		_, err := startRuntime(context.Background(), runtime, &ServiceContext{}, prior, desired)
		Expect(err).ToNot(HaveOccurred())
		Expect(runtime.started).To(Equal(1))
		Expect(runtime.reconciled).To(BeZero())
	})

	It("uses persisted runtime state when the original request used auto-selection", func() {
		priorOptions := desired
		priorOptions.Runtime = ""
		prior := &state.State{Name: "nats", Runtime: "docker", Ready: true, StartOptions: &priorOptions}
		runtime := &recordingRuntime{status: state.StatusRunning}

		result, err := startRuntime(context.Background(), runtime, &ServiceContext{}, prior, desired)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeIdenticalTo(prior))
		Expect(runtime.reconciled).To(BeZero())
	})

	It("preserves generated credentials while preparing a reconciliation", func() {
		stateDir := GinkgoT().TempDir()
		prior := &state.State{
			Name:       "nats",
			Runtime:    "docker",
			Connection: models.Connection{Password: "same-password"},
		}
		Expect(prior.Save(stateDir)).To(Succeed())
		svc, err := newServiceContext("nats", types.Package{}, types.ServiceSpec{
			Credentials: &types.ServiceCredentials{Username: "client"},
		}, Options{StateDir: stateDir})
		Expect(err).ToNot(HaveOccurred())
		Expect(svc.Password).To(Equal("same-password"))
	})

	It("persists generated credentials before any runtime starts", func() {
		stateDir := GinkgoT().TempDir()
		spec := types.ServiceSpec{Credentials: &types.ServiceCredentials{Username: "client"}}
		first, err := newServiceContext("nats", types.Package{}, spec, Options{StateDir: stateDir})
		Expect(err).ToNot(HaveOccurred())
		second, err := newServiceContext("nats", types.Package{}, spec, Options{StateDir: stateDir})
		Expect(err).ToNot(HaveOccurred())
		Expect(second.Password).To(Equal(first.Password))
	})

	It("preserves persisted options when a rerun supplies no overrides", func() {
		prior := &state.State{
			Runtime: "docker",
			StartOptions: &state.StartOptions{
				Runtime: "docker", Version: "3.7.0", Port: 19200,
				Bind: "0.0.0.0", Namespace: "search", DataDir: "/data/opensearch",
				VolumeMode: "host", Parameters: map[string]string{"jvm-memory": "1g"},
			},
		}
		options, err := ResolveOptions(nil)
		Expect(err).ToNot(HaveOccurred())

		Expect(mergePriorOptions(&options, prior)).To(Succeed())
		Expect(options.Runtime).To(Equal(RuntimeDocker))
		Expect(options.Version).To(Equal("3.7.0"))
		Expect(options.Port).To(Equal(19200))
		Expect(options.BindAddress).To(Equal("0.0.0.0"))
		Expect(options.Namespace).To(Equal("search"))
		Expect(options.DataDir).To(Equal("/data/opensearch"))
		Expect(options.VolumeMode).To(Equal(VolumeHost))
		Expect(options.Parameters).To(Equal(map[string]string{"jvm-memory": "1g"}))
	})

	It("updates supplied parameters without resetting other persisted options", func() {
		prior := &state.State{
			Runtime: "docker",
			StartOptions: &state.StartOptions{
				Runtime: "docker", Version: "3.7.0", Port: 19200,
				VolumeMode: "persistent",
				Parameters: map[string]string{"jvm-memory": "512m", "plugins": "analysis-icu"},
			},
		}
		options, err := ResolveOptions([]Option{WithParameters(map[string]string{"jvm-memory": "1g"})})
		Expect(err).ToNot(HaveOccurred())

		Expect(mergePriorOptions(&options, prior)).To(Succeed())
		Expect(options.Version).To(Equal("3.7.0"))
		Expect(options.Port).To(Equal(19200))
		Expect(options.VolumeMode).To(Equal(VolumePersistent))
		Expect(options.Parameters).To(Equal(map[string]string{
			"jvm-memory": "1g",
			"plugins":    "analysis-icu",
		}))
	})
})
