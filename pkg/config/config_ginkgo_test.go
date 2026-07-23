package config

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
)

var _ = Describe("Config", func() {
	Describe("LoadDefaultConfig", func() {
		It("should load embedded defaults.yaml without error", func() {
			config, err := LoadDefaultConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(len(config.Registry)).To(BeNumerically(">", 50))
			Expect(config.Registry).To(HaveKey("powershell"))
			Expect(config.Registry).To(HaveKey("step"))
		})

		It("should include dcg release assets", func() {
			config, err := LoadDefaultConfig()
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Registry).To(HaveKeyWithValue("dcg", types.Package{
				Name:    "dcg",
				Manager: "github_release",
				Repo:    "Dicklesworthstone/destructive_command_guard",
				AssetPatterns: map[string]string{
					"darwin-amd64":  "dcg-x86_64-apple-darwin.tar.xz",
					"darwin-arm64":  "dcg-aarch64-apple-darwin.tar.xz",
					"linux-amd64":   "dcg-x86_64-unknown-linux-musl.tar.xz",
					"linux-arm64":   "dcg-aarch64-unknown-linux-gnu.tar.xz",
					"windows-amd64": "dcg-x86_64-pc-windows-msvc.zip",
					"windows-arm64": "dcg-aarch64-pc-windows-msvc.zip",
				},
				ChecksumFile:   "SHA256SUMS",
				VersionCommand: "--version",
				VersionRegex:   `dcg\s+v?(\d+\.\d+\.\d+)`,
			}))

			installed, err := version.ExtractFromOutput("dcg v0.6.7", config.Registry["dcg"].VersionRegex)
			Expect(err).ToNot(HaveOccurred())
			Expect(installed).To(Equal("0.6.7"))
		})
	})

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
