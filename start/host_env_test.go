package start

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
)

var _ = Describe("host environment", func() {
	const (
		baseOnly  = "BASE_ONLY"
		specKey   = "SPEC_KEY"
		callerKey = "CALLER_KEY"
	)
	hostEnv := map[string]string{baseOnly: "base", specKey: "base", callerKey: "base"}
	specEnv := map[string]string{specKey: "spec-{{.port}}", callerKey: "spec"}
	callerEnv := map[string]string{callerKey: "caller"}
	layered := map[string]string{baseOnly: "base", specKey: "spec-4222", callerKey: "caller"}

	service := func(spec types.ServiceSpec) *ServiceContext {
		return &ServiceContext{Name: "svc", Spec: spec, Opts: Options{HostEnv: hostEnv, Env: callerEnv, Port: 4222}}
	}

	It("layers a binary service's env as HostEnv, then the catalog's, then Env", func() {
		svc := service(types.ServiceSpec{Binary: &types.BinaryRuntime{Command: "/bin/true", Env: specEnv}})
		_, _, env, err := renderCommand(svc, templateData(svc, "localhost:4222", ""))
		Expect(err).ToNot(HaveOccurred())
		Expect(env).To(Equal(layered))
	})

	It("layers a command service's env the same way", func() {
		svc := service(types.ServiceSpec{Command: &types.CommandRuntime{Start: "true", Env: specEnv}})
		env, err := renderHostEnv(svc, "command.env", specEnv, templateData(svc, "localhost:4222", ""))
		Expect(err).ToNot(HaveOccurred())
		Expect(env).To(Equal(layered))
	})

	It("keeps HostEnv out of a docker container", func() {
		svc := service(types.ServiceSpec{Docker: &types.DockerRuntime{Image: "busybox"}})
		env, err := renderDockerEnvironment(svc, templateData(svc, "localhost:4222", ""))
		Expect(err).ToNot(HaveOccurred())
		Expect(env).To(Equal(callerEnv))
	})
})
