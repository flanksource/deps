package start

import (
	"github.com/flanksource/deps/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("Prometheus file discovery", func() {
	It("renders the configured discovery directory as a native scrape job", func() {
		_, spec, ok := config.GetService("prometheus")
		Expect(ok).To(BeTrue())
		parameters, _, err := resolveServiceParameters(*spec, RuntimeBinary, map[string]string{"file-sd-dir": "/example/targets"})
		Expect(err).NotTo(HaveOccurred())
		data, err := render("prometheus.yml", spec.Binary.Files["prometheus.yml"], map[string]any{"port": 9090, "parameters": parameters})
		Expect(err).NotTo(HaveOccurred())
		var document struct {
			Jobs []struct {
				Name      string `yaml:"job_name"`
				Discovery []struct {
					Files   []string `yaml:"files"`
					Refresh string   `yaml:"refresh_interval"`
				} `yaml:"file_sd_configs"`
			} `yaml:"scrape_configs"`
		}
		Expect(yaml.Unmarshal([]byte(data), &document)).To(Succeed())
		Expect(document.Jobs).To(HaveLen(2))
		Expect(document.Jobs[1].Name).To(Equal("file_sd"))
		Expect(document.Jobs[1].Discovery[0].Files).To(Equal([]string{"/example/targets/*.json"}))
		Expect(document.Jobs[1].Discovery[0].Refresh).To(Equal("1s"))
	})
	It("keeps the default self scrape without a discovery directory", func() {
		_, spec, ok := config.GetService("prometheus")
		Expect(ok).To(BeTrue())
		parameters, _, err := resolveServiceParameters(*spec, RuntimeBinary, nil)
		Expect(err).NotTo(HaveOccurred())
		data, err := render("prometheus.yml", spec.Binary.Files["prometheus.yml"], map[string]any{"port": 9090, "parameters": parameters})
		Expect(err).NotTo(HaveOccurred())
		Expect(data).NotTo(ContainSubstring("file_sd"))
	})
})
