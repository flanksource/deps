package github

import (
	"context"
	"fmt"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rate Limit Fallback", func() {
	var mgr *GitHubReleaseManager
	var ctx context.Context

	BeforeEach(func() {
		mgr = NewGitHubReleaseManager()
		ctx = context.Background()
	})

	Describe("handleRateLimitFallback", func() {
		var pkg types.Package
		var plat platform.Platform
		var rateLimitErr error

		BeforeEach(func() {
			pkg = types.Package{
				Name:        "test-tool",
				Repo:        "owner/test-tool",
				Manager:     "github_release",
				URLTemplate: "https://github.com/owner/test-tool/releases/download/{{.tag}}/{{.asset}}",
				AssetPatterns: map[string]string{
					"*": "{{.name}}-{{.os}}-{{.arch}}.tar.gz",
				},
			}
			plat = platform.Platform{OS: "linux", Arch: "amd64"}
			rateLimitErr = fmt.Errorf("API rate limit exceeded")
		})

		Context("with strict checksum enabled", func() {
			It("should return error when rate limited", func() {
				strictCtx := manager.WithStrictChecksum(ctx, true)
				resolution, err := mgr.handleRateLimitFallback(strictCtx, pkg, "1.0.0", plat, rateLimitErr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("strict-checksum"))
				Expect(resolution).To(BeNil())
			})
		})

		Context("with strict checksum disabled", func() {
			It("should build fallback resolution with default version when url_template is configured", func() {
				nonStrictCtx := manager.WithStrictChecksum(ctx, false)
				resolution, err := mgr.handleRateLimitFallback(nonStrictCtx, pkg, "1.0.0", plat, rateLimitErr)
				Expect(err).ToNot(HaveOccurred())
				Expect(resolution).ToNot(BeNil())
				Expect(resolution.Version).To(Equal("1.0.0"))
				Expect(resolution.DownloadURL).To(ContainSubstring("v1.0.0"))
				Expect(resolution.Checksum).To(BeEmpty())
			})

			It("should use fallback_version when specified", func() {
				pkg.FallbackVersion = "0.9.0"
				nonStrictCtx := manager.WithStrictChecksum(ctx, false)
				resolution, err := mgr.handleRateLimitFallback(nonStrictCtx, pkg, "latest", plat, rateLimitErr)
				Expect(err).ToNot(HaveOccurred())
				Expect(resolution).ToNot(BeNil())
				Expect(resolution.Version).To(Equal("0.9.0"))
			})

			It("should work with asset_patterns when url_template is not configured", func() {
				pkg.URLTemplate = ""
				pkg.AssetPatterns = map[string]string{
					"linux-amd64": "test-tool-linux-amd64.tar.gz",
				}
				nonStrictCtx := manager.WithStrictChecksum(ctx, false)
				resolution, err := mgr.handleRateLimitFallback(nonStrictCtx, pkg, "1.0.0", plat, rateLimitErr)
				Expect(err).ToNot(HaveOccurred())
				Expect(resolution).ToNot(BeNil())
				Expect(resolution.DownloadURL).To(Equal("https://github.com/owner/test-tool/releases/download/v1.0.0/test-tool-linux-amd64.tar.gz"))
				Expect(resolution.Checksum).To(BeEmpty())
			})

			It("should fail when no url_template and no asset_patterns configured", func() {
				pkg.URLTemplate = ""
				pkg.AssetPatterns = nil
				nonStrictCtx := manager.WithStrictChecksum(ctx, false)
				resolution, err := mgr.handleRateLimitFallback(nonStrictCtx, pkg, "1.0.0", plat, rateLimitErr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("url_template or asset_patterns"))
				Expect(resolution).To(BeNil())
			})

			It("should fail when asset_pattern contains wildcards", func() {
				pkg.URLTemplate = ""
				pkg.AssetPatterns = map[string]string{
					"*": "test-tool-*-{{.os}}-{{.arch}}.tar.gz",
				}
				nonStrictCtx := manager.WithStrictChecksum(ctx, false)
				resolution, err := mgr.handleRateLimitFallback(nonStrictCtx, pkg, "1.0.0", plat, rateLimitErr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("url_template or asset_patterns"))
				Expect(resolution).To(BeNil())
			})
		})
	})

	Describe("buildFallbackResolution", func() {
		It("should build correct download URL", func() {
			pkg := types.Package{
				Name:        "kubectl",
				Repo:        "kubernetes/kubernetes",
				URLTemplate: "https://dl.k8s.io/release/{{.tag}}/bin/{{.os}}/{{.arch}}/kubectl",
			}
			plat := platform.Platform{OS: "linux", Arch: "amd64"}

			resolution, err := mgr.buildFallbackResolution(pkg, "1.28.0", plat)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolution.DownloadURL).To(Equal("https://dl.k8s.io/release/v1.28.0/bin/linux/amd64/kubectl"))
			Expect(resolution.Checksum).To(BeEmpty())
			Expect(resolution.IsArchive).To(BeFalse())
		})

		It("should detect archives and set binary path", func() {
			pkg := types.Package{
				Name:        "helm",
				Repo:        "helm/helm",
				URLTemplate: "https://get.helm.sh/helm-{{.tag}}-{{.os}}-{{.arch}}.tar.gz",
			}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			resolution, err := mgr.buildFallbackResolution(pkg, "3.14.0", plat)
			Expect(err).ToNot(HaveOccurred())
			Expect(resolution.IsArchive).To(BeTrue())
			Expect(resolution.BinaryPath).ToNot(BeEmpty())
		})
	})

	Describe("isRateLimitError", func() {
		It("should detect rate limit messages in error string", func() {
			Expect(isRateLimitError(fmt.Errorf("API rate limit exceeded"))).To(BeTrue())
			Expect(isRateLimitError(fmt.Errorf("403 Forbidden"))).To(BeTrue())
			Expect(isRateLimitError(fmt.Errorf("rate limit"))).To(BeTrue())
		})

		It("should return false for non-rate-limit errors", func() {
			Expect(isRateLimitError(fmt.Errorf("not found"))).To(BeFalse())
			Expect(isRateLimitError(fmt.Errorf("network error"))).To(BeFalse())
			Expect(isRateLimitError(nil)).To(BeFalse())
		})
	})

	Describe("manager.GetStrictChecksum", func() {
		It("should return true by default", func() {
			Expect(manager.GetStrictChecksum(ctx)).To(BeTrue())
		})

		It("should return the value set via WithStrictChecksum", func() {
			nonStrictCtx := manager.WithStrictChecksum(ctx, false)
			Expect(manager.GetStrictChecksum(nonStrictCtx)).To(BeFalse())

			strictCtx := manager.WithStrictChecksum(ctx, true)
			Expect(manager.GetStrictChecksum(strictCtx)).To(BeTrue())
		})
	})
})

var _ = Describe("GitHubTagsManager Rate Limit Fallback", func() {
	var mgr *GitHubTagsManager
	var ctx context.Context

	BeforeEach(func() {
		mgr = NewGitHubTagsManager()
		ctx = context.Background()
	})

	Describe("handleRateLimitFallback", func() {
		var pkg types.Package
		var plat platform.Platform
		var rateLimitErr error

		BeforeEach(func() {
			pkg = types.Package{
				Name:        "test-tool",
				Repo:        "owner/test-tool",
				Manager:     "github_tags",
				URLTemplate: "https://example.com/releases/{{.tag}}/{{.asset}}",
				AssetPatterns: map[string]string{
					"*": "{{.name}}-{{.os}}-{{.arch}}.tar.gz",
				},
			}
			plat = platform.Platform{OS: "linux", Arch: "amd64"}
			rateLimitErr = fmt.Errorf("API rate limit exceeded")
		})

		Context("with strict checksum enabled", func() {
			It("should return error when rate limited", func() {
				strictCtx := manager.WithStrictChecksum(ctx, true)
				resolution, err := mgr.handleRateLimitFallback(strictCtx, pkg, "1.0.0", plat, rateLimitErr)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("strict-checksum"))
				Expect(resolution).To(BeNil())
			})
		})

		Context("with strict checksum disabled", func() {
			It("should build fallback resolution", func() {
				nonStrictCtx := manager.WithStrictChecksum(ctx, false)
				resolution, err := mgr.handleRateLimitFallback(nonStrictCtx, pkg, "1.0.0", plat, rateLimitErr)
				Expect(err).ToNot(HaveOccurred())
				Expect(resolution).ToNot(BeNil())
				Expect(resolution.Version).To(Equal("1.0.0"))
				Expect(resolution.Checksum).To(BeEmpty())
			})
		})
	})
})
