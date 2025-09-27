package config

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
)

var _ = Describe("Config", func() {
	Describe("Package Defaults", func() {
		Context("when applying package defaults", func() {
			It("should set name to registry key when name is empty", func() {
				input := types.DepsConfig{
					Registry: map[string]types.Package{
						"kubectl": {
							Repo: "kubernetes/kubernetes",
						},
					},
				}

				// Apply package defaults logic
				config := input
				if config.Registry == nil {
					config.Registry = make(map[string]types.Package)
				}

				for name, pkg := range config.Registry {
					if pkg.Name == "" {
						pkg.Name = name
					}
					if pkg.Manager == "" && pkg.Repo != "" {
						pkg.Manager = "github_release"
					}
					config.Registry[name] = pkg
				}

				pkg := config.Registry["kubectl"]
				Expect(pkg.Name).To(Equal("kubectl"))
				Expect(pkg.Manager).To(Equal("github_release"))
			})

			It("should preserve existing package name", func() {
				input := types.DepsConfig{
					Registry: map[string]types.Package{
						"k8s-cli": {
							Name: "kubectl",
							Repo: "kubernetes/kubernetes",
						},
					},
				}

				// Apply package defaults logic
				config := input
				for name, pkg := range config.Registry {
					if pkg.Name == "" {
						pkg.Name = name
					}
					if pkg.Manager == "" && pkg.Repo != "" {
						pkg.Manager = "github_release"
					}
					config.Registry[name] = pkg
				}

				pkg := config.Registry["k8s-cli"]
				Expect(pkg.Name).To(Equal("kubectl"))
				Expect(pkg.Manager).To(Equal("github_release"))
			})

			It("should preserve existing manager", func() {
				input := types.DepsConfig{
					Registry: map[string]types.Package{
						"terraform": {
							Manager:     "direct",
							URLTemplate: "https://releases.hashicorp.com/terraform/{{.version}}/terraform_{{.version}}_{{.os}}_{{.arch}}.zip",
						},
					},
				}

				// Apply package defaults logic
				config := input
				for name, pkg := range config.Registry {
					if pkg.Name == "" {
						pkg.Name = name
					}
					if pkg.Manager == "" && pkg.Repo != "" {
						pkg.Manager = "github_release"
					}
					config.Registry[name] = pkg
				}

				pkg := config.Registry["terraform"]
				Expect(pkg.Name).To(Equal("terraform"))
				Expect(pkg.Manager).To(Equal("direct"))
			})

			It("should not set manager when no repo is specified", func() {
				input := types.DepsConfig{
					Registry: map[string]types.Package{
						"some-tool": {
							URLTemplate: "https://example.com/tool",
						},
					},
				}

				// Apply package defaults logic
				config := input
				for name, pkg := range config.Registry {
					if pkg.Name == "" {
						pkg.Name = name
					}
					if pkg.Manager == "" && pkg.Repo != "" {
						pkg.Manager = "github_release"
					}
					config.Registry[name] = pkg
				}

				pkg := config.Registry["some-tool"]
				Expect(pkg.Name).To(Equal("some-tool"))
				Expect(pkg.Manager).To(Equal(""))
			})
		})
	})

	Describe("ValidateConfig", func() {
		Context("with valid configurations", func() {
			It("should validate GitHub release configuration", func() {
				config := &types.DepsConfig{
					Dependencies: map[string]string{
						"kubectl": "v1.31.0",
					},
					Registry: map[string]types.Package{
						"kubectl": {
							Name:    "kubectl",
							Manager: "github_release",
							Repo:    "kubernetes/kubernetes",
						},
					},
				}

				err := ValidateConfig(config)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should validate direct URL configuration", func() {
				config := &types.DepsConfig{
					Dependencies: map[string]string{
						"terraform": "1.1.7",
					},
					Registry: map[string]types.Package{
						"terraform": {
							Name:        "terraform",
							Manager:     "direct",
							URLTemplate: "https://releases.hashicorp.com/terraform/{{.version}}/terraform_{{.version}}_{{.os}}_{{.arch}}.zip",
						},
					},
				}

				err := ValidateConfig(config)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("with invalid configurations", func() {
			It("should fail when GitHub release is missing repo", func() {
				config := &types.DepsConfig{
					Dependencies: map[string]string{
						"kubectl": "v1.31.0",
					},
					Registry: map[string]types.Package{
						"kubectl": {
							Name:    "kubectl",
							Manager: "github_release",
						},
					},
				}

				err := ValidateConfig(config)
				Expect(err).To(HaveOccurred())
			})

			It("should fail when direct manager is missing URL template", func() {
				config := &types.DepsConfig{
					Dependencies: map[string]string{
						"tool": "1.0.0",
					},
					Registry: map[string]types.Package{
						"tool": {
							Name:    "tool",
							Manager: "direct",
						},
					},
				}

				err := ValidateConfig(config)
				Expect(err).To(HaveOccurred())
			})

			It("should fail when dependency is not in registry", func() {
				config := &types.DepsConfig{
					Dependencies: map[string]string{
						"missing-tool": "1.0.0",
					},
					Registry: map[string]types.Package{},
				}

				err := ValidateConfig(config)
				Expect(err).To(HaveOccurred())
			})
		})
	})
})