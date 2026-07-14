package start

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	mobyclient "github.com/moby/moby/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/start/state"
)

var _ = Describe("docker runtime", Label("integration"), func() {
	var stateDir string
	ctx := context.Background()

	BeforeEach(func() {
		stateDir = GinkgoT().TempDir()
	})

	It("starts, reuses and stops valkey", func() {
		instance, err := Start(ctx, "valkey", WithStateDir(stateDir), WithRuntime(RuntimeDocker), WithVolumeMode(VolumePersistent))
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { _ = Stop(ctx, "valkey", WithStateDir(stateDir)) })

		Expect(instance.Connection.Type).To(Equal("redis"))
		Expect(instance.Connection.URL).To(HavePrefix("redis://"))
		Expect(instance.Connection.Password).ToNot(BeEmpty())
		Expect(instance.State.ContainerID).ToNot(BeEmpty())

		status, err := Status(ctx, "valkey", WithStateDir(stateDir))
		Expect(err).ToNot(HaveOccurred())
		Expect(status).To(Equal(state.StatusRunning))

		reused, err := Start(ctx, "valkey", WithStateDir(stateDir), WithRuntime(RuntimeDocker), WithVolumeMode(VolumePersistent))
		Expect(err).ToNot(HaveOccurred())
		Expect(reused.State.ContainerID).To(Equal(instance.State.ContainerID))
		Expect(reused.Connection.URL).To(Equal(instance.Connection.URL))
		Expect(reused.Connection.Password).To(Equal(instance.Connection.Password))

		updated, err := Start(ctx, "valkey", WithStateDir(stateDir), WithRuntime(RuntimeDocker), WithVolumeMode(VolumePersistent), WithPort(16380))
		Expect(err).ToNot(HaveOccurred())
		Expect(updated.State.ContainerID).ToNot(Equal(instance.State.ContainerID))
		Expect(updated.Connection.URL).To(ContainSubstring(":16380"))
		Expect(updated.Connection.Password).To(Equal(instance.Connection.Password))

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

	It("recreates OpenSearch with updated JVM memory and the same password", func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		port := listener.Addr().(*net.TCPAddr).Port
		Expect(listener.Close()).To(Succeed())
		name := fmt.Sprintf("opensearch-integration-%d", time.Now().UnixNano())
		pkg, spec, ok := config.GetService("opensearch")
		Expect(ok).To(BeTrue())
		runtime := &dockerRuntime{}
		docker, err := runtime.client()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = docker.ContainerRemove(ctx, containerName(name), mobyclient.ContainerRemoveOptions{Force: true})
		})

		newContext := func(memory string) *ServiceContext {
			parameters, persisted, resolveErr := resolveServiceParameters(*spec, RuntimeDocker, map[string]string{"jvm-memory": memory})
			Expect(resolveErr).ToNot(HaveOccurred())
			svc, contextErr := newServiceContext(name, pkg, *spec, Options{
				StateDir: stateDir, Port: port, VolumeMode: VolumePersistent,
				WaitTimeout: 3 * time.Minute, Parameters: persisted,
			})
			Expect(contextErr).ToNot(HaveOccurred())
			svc.Parameters = parameters
			return svc
		}

		initialService := newContext("512m")
		initial, err := runtime.Start(ctx, initialService)
		Expect(err).ToNot(HaveOccurred())
		initial.Name, initial.Runtime = name, string(RuntimeDocker)
		updatedService := newContext("1g")
		updated, err := runtime.Reconcile(ctx, updatedService, initial)
		Expect(err).ToNot(HaveOccurred())
		Expect(updated.ContainerID).ToNot(Equal(initial.ContainerID))
		Expect(updatedService.Password).To(Equal(initialService.Password))

		inspect, err := docker.ContainerInspect(ctx, updated.ContainerID, mobyclient.ContainerInspectOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(inspect.Container.Config).ToNot(BeNil())
		Expect(inspect.Container.Config.Env).To(ContainElement("OPENSEARCH_JAVA_OPTS=-Xms1g -Xmx1g"))
		live, err := runtime.InspectConfig(ctx, updatedService, &state.State{ContainerID: updated.ContainerID})
		Expect(err).ToNot(HaveOccurred())
		Expect(live.Parameters).To(Equal(map[string]string{"jvm-memory": "1g"}))
		Expect(live.Volume).To(Equal(&state.Volume{Mode: "persistent", Source: containerName(name) + "-data", Target: "/usr/share/opensearch/data"}))
	})
})
