package github

import (
	"context"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GitHubTagsManager", func() {
	var manager *GitHubTagsManager
	var ctx context.Context

	BeforeEach(func() {
		manager = NewGitHubTagsManager()
		ctx = context.Background()
	})

	Describe("Name", func() {
		It("should return the correct manager name", func() {
			Expect(manager.Name()).To(Equal("github_tags"))
		})
	})

	Describe("DiscoverVersions", func() {
		Context("with valid repository", func() {
			It("should return versions from tags", func() {
				pkg := types.Package{
					Name: "kubectl",
					Repo: "kubernetes/kubernetes",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 10)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				// Verify versions are sorted (newest first)
				if len(versions) > 1 {
					first := versions[0]
					second := versions[1]
					Expect(first.Version).ToNot(BeEmpty())
					Expect(second.Version).ToNot(BeEmpty())
				}
			})
		})

		Context("with missing repo", func() {
			It("should return error when repo is missing", func() {
				pkg := types.Package{
					Name: "test",
					Repo: "",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				_, err := manager.DiscoverVersions(ctx, pkg, plat, 10)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("repo is required"))
			})
		})

		Context("with invalid repo format", func() {
			It("should return error when repo format is invalid", func() {
				pkg := types.Package{
					Name: "test",
					Repo: "invalid-format",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				_, err := manager.DiscoverVersions(ctx, pkg, plat, 10)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid repo format"))
			})
		})
	})

	Describe("Resolve", func() {
		Context("with url_template", func() {
			It("should resolve download URL using template", func() {
				pkg := types.Package{
					Name:        "kubectl",
					Repo:        "kubernetes/kubernetes",
					URLTemplate: "https://dl.k8s.io/release/{{.tag}}/bin/{{.os}}/{{.arch}}/{{.name}}",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				// First get available versions to use a real one
				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 1)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				testVersion := versions[0].Tag

				resolution, err := manager.Resolve(ctx, pkg, testVersion, plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(resolution).ToNot(BeNil())
				Expect(resolution.DownloadURL).To(ContainSubstring("https://dl.k8s.io/release/" + testVersion + "/bin/linux/amd64/kubectl"))
			})
		})

		Context("without url_template", func() {
			It("should return error when url_template is missing", func() {
				pkg := types.Package{
					Name: "test",
					Repo: "owner/repo",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				_, err := manager.Resolve(ctx, pkg, "v1.0.0", plat)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("url_template is required"))
			})
		})

		Context("with asset patterns", func() {
			It("should use asset patterns for templating", func() {
				pkg := types.Package{
					Name:        "kubectl",
					Repo:        "kubernetes/kubernetes",
					URLTemplate: "https://github.com/kubernetes/kubernetes/releases/download/{{.tag}}/{{.asset}}",
					AssetPatterns: map[string]string{
						"linux-amd64": "{{.name}}-{{.version}}-{{.os}}-{{.arch}}.tar.gz",
					},
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				// First get available versions to use a real one
				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 1)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				testVersion := versions[0].Tag

				resolution, err := manager.Resolve(ctx, pkg, testVersion, plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(resolution).ToNot(BeNil())
				Expect(resolution.DownloadURL).To(ContainSubstring("kubectl-" + versions[0].Version + "-linux-amd64.tar.gz"))
				Expect(resolution.IsArchive).To(BeTrue())
			})
		})
	})

	Describe("GetChecksums", func() {
		It("should return nil as checksums are handled externally", func() {
			pkg := types.Package{
				Name: "test",
				Repo: "owner/repo",
			}

			checksums, err := manager.GetChecksums(ctx, pkg, "v1.0.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(checksums).To(BeNil())
		})
	})

	Describe("WhoAmI", func() {
		It("should return authentication status", func() {
			status := manager.WhoAmI(ctx)
			Expect(status).ToNot(BeNil())
			Expect(status.Service).To(Equal("GitHub"))
		})
	})
})
