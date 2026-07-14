package start

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

var _ = Describe("helm args", func() {
	newSvc := func() *ServiceContext {
		svc := testContext(postgresSpec(), Options{Namespace: "dev", WaitTimeout: 2 * time.Minute})
		svc.RunDir = GinkgoT().TempDir()
		return svc
	}

	It("defaults the release name to deps-<service>", func() {
		release, data, err := helmRelease(newSvc())
		Expect(err).ToNot(HaveOccurred())
		Expect(release).To(Equal("deps-postgres"))
		Expect(data["release"]).To(Equal("deps-postgres"))
	})

	It("builds upgrade --install args with repo, namespace and wait", func() {
		svc := newSvc()
		svc.Spec.Helm.Set = map[string]string{"settings.superuserPassword": "{{.password}}"}
		release, data, err := helmRelease(svc)
		Expect(err).ToNot(HaveOccurred())
		args, err := buildHelmArgs(svc, data, release)
		Expect(err).ToNot(HaveOccurred())
		Expect(args[:12]).To(Equal([]string{
			"upgrade", "--install", "deps-postgres", "postgres",
			"--namespace", "dev", "--create-namespace",
			"--wait", "--timeout", "2m0s",
			"--repo", "https://groundhog2k.github.io/helm-charts/",
		}))
		Expect(args).To(ContainElements("--description", "--set", "settings.superuserPassword=sekret"))
	})

	It("writes rendered values to the run dir", func() {
		svc := newSvc()
		svc.Spec.Helm.Values = "auth:\n  database: {{.database}}\n"
		release, data, err := helmRelease(svc)
		Expect(err).ToNot(HaveOccurred())
		args, err := buildHelmArgs(svc, data, release)
		Expect(err).ToNot(HaveOccurred())
		Expect(args).To(ContainElements("-f", svc.RunDir+"/values.yaml"))

		content, err := readFile(svc.RunDir + "/values.yaml")
		Expect(err).ToNot(HaveOccurred())
		Expect(content).To(Equal("auth:\n  database: postgres\n"))
	})

	It("omits empty optional settings and applies the primary port last", func() {
		svc := newSvc()
		svc.Opts.Port = 15432
		svc.Parameters = map[string]any{"cpu-request": ""}
		svc.Spec.Helm.Set = map[string]string{
			"resources.requests.cpu": `{{index .parameters "cpu-request"}}`,
		}
		svc.Spec.Helm.PortSet = map[string]string{"service.port": "{{.port}}"}
		release, data, err := helmRelease(svc)
		Expect(err).ToNot(HaveOccurred())
		args, err := buildHelmArgs(svc, data, release)
		Expect(err).ToNot(HaveOccurred())
		Expect(args[len(args)-2:]).To(Equal([]string{"--set", "service.port=15432"}))
		Expect(args).ToNot(ContainElement("resources.requests.cpu="))
	})
})
