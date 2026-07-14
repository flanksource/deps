package start

import (
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/config"
)

var _ = Describe("catalog service parameters", func() {
	It("renders resource and primary port settings for every Helm service", func() {
		for _, name := range ServiceNames() {
			_, spec, ok := config.GetService(name)
			Expect(ok).To(BeTrue())
			if spec.Helm == nil {
				continue
			}

			primary, ok := spec.PrimaryPort()
			Expect(ok).To(BeTrue(), name)
			port := primary.Port + 10000
			parameters, _, err := resolveServiceParameters(*spec, RuntimeHelm, map[string]string{"cpu-request": "137m"})
			Expect(err).ToNot(HaveOccurred(), name)
			svc := &ServiceContext{
				Name:       name,
				Spec:       *spec,
				Opts:       Options{Namespace: "dev", Port: port, WaitTimeout: 2 * time.Minute},
				Version:    "1.0.0",
				Password:   "sekret",
				RunDir:     GinkgoT().TempDir(),
				Parameters: parameters,
			}
			release, data, err := helmRelease(svc)
			Expect(err).ToNot(HaveOccurred(), name)
			args, err := buildHelmArgs(svc, data, release)
			Expect(err).ToNot(HaveOccurred(), name)
			joined := strings.Join(args, "\n")
			Expect(joined).To(ContainSubstring(spec.Helm.ResourcePrefix+".requests.cpu=137m"), name)
			Expect(joined).To(ContainSubstring(strconv.Itoa(port)), name)
		}
	})

	It("removes disabled NATS JetStream arguments", func() {
		_, spec, ok := config.GetService("nats")
		Expect(ok).To(BeTrue())
		parameters, _, err := resolveServiceParameters(*spec, RuntimeBinary, map[string]string{"jetstream": "false"})
		Expect(err).ToNot(HaveOccurred())
		svc := &ServiceContext{Name: "nats", Spec: *spec, Opts: Options{}, Parameters: parameters}
		_, args, _, err := renderCommand(svc, templateData(svc, "localhost:4222", ""))
		Expect(err).ToNot(HaveOccurred())
		Expect(args).ToNot(ContainElements("-js", "-sd"))

		dockerArgs, err := renderArguments("docker.cmd", spec.Docker.Args, templateData(svc, "localhost:4222", ""))
		Expect(err).ToNot(HaveOccurred())
		Expect(dockerArgs).ToNot(ContainElements("-js", "-sd", "/data"))
	})

	It("renders updated OpenSearch JVM memory into the Docker environment", func() {
		_, spec, ok := config.GetService("opensearch")
		Expect(ok).To(BeTrue())
		parameters, _, err := resolveServiceParameters(*spec, RuntimeDocker, map[string]string{"jvm-memory": "1g"})
		Expect(err).ToNot(HaveOccurred())
		svc := &ServiceContext{Name: "opensearch", Spec: *spec, Opts: Options{}, Parameters: parameters}

		env, err := renderDockerEnvironment(svc, templateData(svc, "localhost:9200", ""))
		Expect(err).ToNot(HaveOccurred())
		Expect(env).To(HaveKeyWithValue("OPENSEARCH_JAVA_OPTS", "-Xms1g -Xmx1g"))
	})
})
