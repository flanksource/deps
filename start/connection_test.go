package start

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/start/state"
)

func postgresSpec() types.ServiceSpec {
	return types.ServiceSpec{
		Type:        "postgres",
		Ports:       []types.ServicePort{{Name: "postgres", Port: 5432}},
		Credentials: &types.ServiceCredentials{Username: "postgres", Database: "postgres"},
		URL:         "postgres://{{.username}}:{{.password}}@{{.host}}/{{.database}}?sslmode=disable",
		Binary:      &types.BinaryRuntime{Command: "bin/postgres"},
		Docker:      &types.DockerRuntime{Image: "postgres:{{.major}}"},
		Helm: &types.HelmRuntime{
			Chart:   "postgres",
			Repo:    "https://groundhog2k.github.io/helm-charts/",
			Secret:  &types.SecretRef{Name: "{{.release}}", Key: "POSTGRES_PASSWORD"},
			Service: &types.ServiceRef{Name: "{{.release}}", Port: 5432},
		},
	}
}

func testContext(spec types.ServiceSpec, opts Options) *ServiceContext {
	svc := &ServiceContext{
		Name:     "postgres",
		Spec:     spec,
		Opts:     opts,
		Version:  "17.5.0",
		Username: "postgres",
		Password: "sekret",
		Database: "postgres",
		DataDir:  "/state/postgres/data",
		RunDir:   "/state/postgres/run",
	}
	return svc
}

var _ = Describe("BuildConnection", func() {
	It("builds a localhost DSN with inline credentials for the binary runtime", func() {
		svc := testContext(postgresSpec(), Options{})
		conn, err := BuildConnection(svc, &state.State{}, RuntimeBinary)
		Expect(err).ToNot(HaveOccurred())
		Expect(conn.Type).To(Equal("postgres"))
		Expect(conn.URL).To(Equal("postgres://postgres:sekret@localhost:5432/postgres?sslmode=disable"))
		Expect(conn.Username).To(Equal("postgres"))
		Expect(conn.Password).To(Equal("sekret"))
		Expect(conn.Properties["database"]).To(Equal("postgres"))
	})

	It("uses the host port override in the DSN", func() {
		svc := testContext(postgresSpec(), Options{Port: 15432})
		conn, err := BuildConnection(svc, &state.State{}, RuntimeDocker)
		Expect(err).ToNot(HaveOccurred())
		Expect(conn.URL).To(Equal("postgres://postgres:sekret@localhost:15432/postgres?sslmode=disable"))
	})

	It("emits svc:// URL and secret:// credentials for the helm runtime", func() {
		svc := testContext(postgresSpec(), Options{Namespace: "dev"})
		conn, err := BuildConnection(svc, &state.State{HelmRelease: "deps-postgres"}, RuntimeHelm)
		Expect(err).ToNot(HaveOccurred())
		Expect(conn.URL).To(Equal("svc://deps-postgres.dev:5432"))
		Expect(conn.Password).To(Equal("secret://deps-postgres/POSTGRES_PASSWORD"))
		Expect(conn.Username).To(Equal("postgres"), "no username_key -> literal username")
		Expect(conn.Namespace).To(Equal("dev"))
	})

	It("references the username key when the secret declares one", func() {
		spec := postgresSpec()
		spec.Helm.Secret.UsernameKey = "POSTGRES_USER"
		svc := testContext(spec, Options{Namespace: "dev"})
		conn, err := BuildConnection(svc, &state.State{HelmRelease: "deps-postgres"}, RuntimeHelm)
		Expect(err).ToNot(HaveOccurred())
		Expect(conn.Username).To(Equal("secret://deps-postgres/POSTGRES_USER"))
	})

	It("renders templated properties with the runtime host", func() {
		spec := types.ServiceSpec{
			Type:        "aws",
			Ports:       []types.ServicePort{{Name: "edge", Port: 4566}},
			URL:         "http://{{.host}}",
			Credentials: &types.ServiceCredentials{Username: "test", Password: "test"},
			Properties:  map[string]string{"endpoint": "http://{{.host}}", "region": "us-east-1"},
			Docker:      &types.DockerRuntime{Image: "ministackorg/ministack:{{.version}}"},
		}
		svc := testContext(spec, Options{})
		svc.Name = "ministack"
		conn, err := BuildConnection(svc, &state.State{}, RuntimeDocker)
		Expect(err).ToNot(HaveOccurred())
		Expect(conn.URL).To(Equal("http://localhost:4566"))
		Expect(conn.Properties["endpoint"]).To(Equal("http://localhost:4566"))
		Expect(conn.Properties["region"]).To(Equal("us-east-1"))
	})

	It("fails loudly on unknown template variables", func() {
		spec := postgresSpec()
		spec.URL = "postgres://{{.doesNotExist}}"
		svc := testContext(spec, Options{})
		_, err := BuildConnection(svc, &state.State{}, RuntimeBinary)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("selectRuntime", func() {
	It("prefers binary over docker over helm", func() {
		kind, err := selectRuntime(postgresSpec(), "", "darwin", "arm64")
		Expect(err).ToNot(HaveOccurred())
		Expect(kind).To(Equal(RuntimeBinary))
	})

	It("honors an explicit supported runtime", func() {
		kind, err := selectRuntime(postgresSpec(), RuntimeHelm, "darwin", "arm64")
		Expect(err).ToNot(HaveOccurred())
		Expect(kind).To(Equal(RuntimeHelm))
	})

	It("rejects unsupported runtimes listing the supported ones", func() {
		spec := postgresSpec()
		spec.Binary = nil
		_, err := selectRuntime(spec, RuntimeBinary, "darwin", "arm64")
		Expect(err).To(MatchError(ContainSubstring("docker, helm")))
	})

	It("skips platform-filtered runtimes during auto-selection", func() {
		spec := postgresSpec()
		spec.Binary.Platforms = []string{"linux-*"}
		kind, err := selectRuntime(spec, "", "darwin", "arm64")
		Expect(err).ToNot(HaveOccurred())
		Expect(kind).To(Equal(RuntimeDocker), "binary is linux-only, docker is next")

		kind, err = selectRuntime(spec, "", "linux", "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(kind).To(Equal(RuntimeBinary))
	})
})
