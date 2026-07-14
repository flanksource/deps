package config

import (
	"regexp"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
)

var identifierSafe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)

var _ = Describe("Service registry", func() {
	var config *types.DepsConfig

	BeforeEach(func() {
		var err error
		config, err = LoadDefaultConfig()
		Expect(err).ToNot(HaveOccurred())
	})

	It("attaches service specs to existing registry entries", func() {
		pg, ok := config.Registry["postgres"]
		Expect(ok).To(BeTrue())
		Expect(pg.Service).ToNot(BeNil())
		Expect(pg.Service.Type).To(Equal("postgres"))
		Expect(pg.Service.Binary).ToNot(BeNil(), "postgres has an installable artifact")
		Expect(pg.Manager).To(Equal("maven"), "service spec must not clobber install metadata")
	})

	It("creates service-only entries for services without artifacts", func() {
		for _, name := range []string{"mysql", "mssql", "valkey", "rabbitmq", "ministack"} {
			pkg, ok := config.Registry[name]
			Expect(ok).To(BeTrue(), name)
			Expect(pkg.Service).ToNot(BeNil(), name)
			Expect(pkg.Service.Binary).To(BeNil(), name+" has no binary runtime")
			Expect(pkg.Name).To(Equal(name))
		}
	})

	It("passes config validation with service-only entries present", func() {
		Expect(ValidateConfig(config)).To(Succeed())
	})

	It("defines consistent specs for every service", func() {
		services := map[string]*types.ServiceSpec{}
		for name, pkg := range config.Registry {
			if pkg.Service != nil {
				services[name] = pkg.Service
			}
		}
		Expect(len(services)).To(BeNumerically(">=", 22))

		for name, spec := range services {
			Expect(spec.Type).ToNot(BeEmpty(), name)
			Expect(spec.Runtimes()).ToNot(BeEmpty(), name)
			Expect(spec.URL).ToNot(BeEmpty(), name)

			_, hasPrimary := spec.PrimaryPort()
			Expect(hasPrimary).To(BeTrue(), name)

			portNames := map[string]bool{}
			for _, p := range spec.Ports {
				Expect(p.Name).To(MatchRegexp(identifierSafe.String()), "%s port %q must be identifier-safe for templating", name, p.Name)
				Expect(p.Port).To(BeNumerically(">", 0), name)
				portNames[p.Name] = true
			}

			for _, h := range []*types.HealthCheck{spec.Health, healthOf(spec.Binary), healthOf(spec.Docker), healthOf(spec.Helm)} {
				if h != nil && h.Port != "" {
					Expect(portNames).To(HaveKey(h.Port), "%s health references unknown port %q", name, h.Port)
				}
			}

			if spec.Helm != nil {
				Expect(spec.Helm.Chart).ToNot(BeEmpty(), name)
				Expect(spec.Helm.Chart).ToNot(ContainSubstring("bitnami"), name)
				Expect(spec.Helm.ResourcePrefix).ToNot(BeEmpty(), name)
				Expect(spec.Helm.PortSet).ToNot(BeEmpty(), name)
				for _, parameter := range []string{"cpu-request", "cpu-limit", "memory-request", "memory-limit"} {
					Expect(spec.Parameters).To(HaveKey(parameter), "%s must expose --%s", name, parameter)
				}
				if spec.Helm.Secret != nil {
					Expect(spec.Helm.Secret.Name).ToNot(BeEmpty(), name)
					Expect(spec.Helm.Secret.Key).ToNot(BeEmpty(), name)
				}
				if spec.Docker != nil && spec.Docker.DataPath != "" {
					Expect(spec.Helm.Volume).ToNot(BeNil(), "%s must map its primary helm data volume", name)
					Expect(spec.Helm.Volume.Modes).To(HaveKey("persistent"), "%s must support persistent helm storage", name)
				}
			}
			if spec.Docker != nil {
				Expect(spec.Docker.Image).ToNot(BeEmpty(), name)
				Expect(spec.Docker.Image).ToNot(ContainSubstring("bitnami"), name)
			}
		}
	})

	DescribeTable("defines service-specific parameters",
		func(service string, parameters ...string) {
			_, spec, ok := GetService(service)
			Expect(ok).To(BeTrue())
			for _, parameter := range parameters {
				Expect(spec.Parameters).To(HaveKey(parameter))
			}
		},
		Entry("OpenSearch JVM heap", "opensearch", "jvm-memory"),
		Entry("Elasticsearch JVM heap", "elasticsearch", "jvm-memory"),
		Entry("NATS JetStream", "nats", "jetstream"),
		Entry("Rclone region", "rclone", "region"),
		Entry("Ministack region", "ministack", "region"),
	)

	It("resolves services through GetService", func() {
		pkg, spec, ok := GetService("postgres")
		Expect(ok).To(BeTrue())
		Expect(spec.Type).To(Equal("postgres"))
		Expect(pkg.Name).To(Equal("postgres-embedded"))

		_, _, ok = GetService("jq")
		Expect(ok).To(BeFalse(), "jq is not a service")
	})

	It("lets user config override a whole service block", func() {
		defaultPkg := config.Registry["postgres"]
		userPkg := types.Package{Service: &types.ServiceSpec{Type: "postgres", URL: "custom://{{.host}}"}}
		merged := mergePackage(defaultPkg, userPkg)
		Expect(merged.Service.URL).To(Equal("custom://{{.host}}"))
		Expect(merged.Manager).To(Equal("maven"))
	})
})

func healthOf(v any) *types.HealthCheck {
	switch r := v.(type) {
	case *types.BinaryRuntime:
		if r != nil {
			return r.Health
		}
	case *types.DockerRuntime:
		if r != nil {
			return r.Health
		}
	case *types.HelmRuntime:
		if r != nil {
			return r.Health
		}
	}
	return nil
}
