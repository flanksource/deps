package start

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/start/state"
)

var _ = Describe("docker runtime", Label("integration"), func() {
	var stateDir string
	ctx := context.Background()

	BeforeEach(func() {
		stateDir = GinkgoT().TempDir()
	})

	It("starts, reuses and stops valkey", func() {
		instance, err := Start(ctx, "valkey", WithStateDir(stateDir), WithRuntime(RuntimeDocker))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = Stop(ctx, "valkey", WithStateDir(stateDir)) })

		Expect(instance.Connection.Type).To(Equal("redis"))
		Expect(instance.Connection.URL).To(HavePrefix("redis://"))
		Expect(instance.Connection.Password).ToNot(BeEmpty())
		Expect(instance.State.ContainerID).ToNot(BeEmpty())

		status, err := Status(ctx, "valkey", WithStateDir(stateDir))
		Expect(err).ToNot(HaveOccurred())
		Expect(status).To(Equal(state.StatusRunning))

		reused, err := Start(ctx, "valkey", WithStateDir(stateDir), WithRuntime(RuntimeDocker))
		Expect(err).ToNot(HaveOccurred())
		Expect(reused.State.ContainerID).To(Equal(instance.State.ContainerID))
		Expect(reused.Connection.URL).To(Equal(instance.Connection.URL))

		Expect(Stop(ctx, "valkey", WithStateDir(stateDir))).To(Succeed())
		status, err = Status(ctx, "valkey", WithStateDir(stateDir))
		Expect(err).ToNot(HaveOccurred())
		Expect(status).To(Equal(state.StatusStopped))
	})

	It("rejects unsupported runtimes with a helpful error", func() {
		_, err := Start(ctx, "valkey", WithStateDir(stateDir), WithRuntime(RuntimeBinary))
		Expect(err).To(HaveOccurred())
		Expect(strings.Contains(err.Error(), "docker, helm")).To(BeTrue())
	})
})
