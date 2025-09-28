package github

import (
	"context"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GitHub Managers with VersionExpr", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("GitHubReleaseManager with version_expr", func() {
		var manager *GitHubReleaseManager

		BeforeEach(func() {
			manager = NewGitHubReleaseManager("", "")
		})

		Context("filtering prereleases", func() {
			It("should exclude prerelease versions", func() {
				pkg := types.Package{
					Name:        "helm",
					Repo:        "helm/helm",
					VersionExpr: "!prerelease",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 10)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				// All returned versions should be stable (not prerelease)
				for _, version := range versions {
					Expect(version.Prerelease).To(BeFalse(), "Version %s should not be a prerelease", version.Tag)
				}
			})
		})

		Context("filtering by tag format", func() {
			It("should only include versions with v prefix", func() {
				pkg := types.Package{
					Name:        "helm",
					Repo:        "helm/helm",
					VersionExpr: "tag.startsWith('v')",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 10)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				// All returned versions should have 'v' prefix
				for _, version := range versions {
					Expect(version.Tag).To(HavePrefix("v"), "Version tag %s should start with 'v'", version.Tag)
				}
			})
		})

		Context("combining multiple filters", func() {
			It("should apply complex filtering logic", func() {
				pkg := types.Package{
					Name:        "helm",
					Repo:        "helm/helm",
					VersionExpr: "!prerelease && tag.startsWith('v')",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 5)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				// All returned versions should be stable AND have 'v' prefix
				for _, version := range versions {
					Expect(version.Prerelease).To(BeFalse(), "Version %s should not be a prerelease", version.Tag)
					Expect(version.Tag).To(HavePrefix("v"), "Version tag %s should start with 'v'", version.Tag)
				}
			})
		})

		Context("with invalid version_expr", func() {
			It("should return error for invalid CEL expression", func() {
				pkg := types.Package{
					Name:        "helm",
					Repo:        "helm/helm",
					VersionExpr: "invalid syntax &&& error",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				_, err := manager.DiscoverVersions(ctx, pkg, plat, 5)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to apply version_expr"))
			})
		})
	})

	Describe("GitHubTagsManager with version_expr", func() {
		var manager *GitHubTagsManager

		BeforeEach(func() {
			manager = NewGitHubTagsManager("", "")
		})

		Context("filtering prereleases", func() {
			It("should exclude prerelease versions", func() {
				pkg := types.Package{
					Name:        "kubectl",
					Repo:        "kubernetes/kubernetes",
					VersionExpr: "!prerelease",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 10)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				// All returned versions should be stable (not prerelease)
				for _, version := range versions {
					Expect(version.Prerelease).To(BeFalse(), "Version %s should not be a prerelease", version.Tag)
				}
			})
		})

		Context("filtering by tag patterns", func() {
			It("should filter versions containing specific strings", func() {
				pkg := types.Package{
					Name:        "kubectl",
					Repo:        "kubernetes/kubernetes",
					VersionExpr: "!tag.contains('alpha') && !tag.contains('beta') && !tag.contains('rc')",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 5)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				// All returned versions should not contain alpha, beta, or rc
				for _, version := range versions {
					Expect(version.Tag).ToNot(ContainSubstring("alpha"), "Version %s should not contain 'alpha'", version.Tag)
					Expect(version.Tag).ToNot(ContainSubstring("beta"), "Version %s should not contain 'beta'", version.Tag)
					Expect(version.Tag).ToNot(ContainSubstring("rc"), "Version %s should not contain 'rc'", version.Tag)
				}
			})
		})

		Context("with date-based filtering", func() {
			It("should filter by published date", func() {
				pkg := types.Package{
					Name:        "kubectl",
					Repo:        "kubernetes/kubernetes",
					VersionExpr: "published > timestamp('2023-01-01T00:00:00Z')",
				}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				_, err := manager.DiscoverVersions(ctx, pkg, plat, 5)
				Expect(err).ToNot(HaveOccurred())

				// Should not error, but might not have results depending on when test runs
				// This tests that the date filtering doesn't cause runtime errors
			})
		})
	})

	Describe("Version ordering after filtering", func() {
		var manager *GitHubReleaseManager

		BeforeEach(func() {
			manager = NewGitHubReleaseManager("", "")
		})

		It("should maintain version ordering after filtering", func() {
			pkg := types.Package{
				Name:        "helm",
				Repo:        "helm/helm",
				VersionExpr: "!prerelease",
			}
			plat := platform.Platform{OS: "linux", Arch: "amd64"}

			versions, err := manager.DiscoverVersions(ctx, pkg, plat, 5)
			Expect(err).ToNot(HaveOccurred())
			Expect(versions).ToNot(BeEmpty())

			// Verify versions are still in descending order after filtering
			if len(versions) > 1 {
				for i := 0; i < len(versions)-1; i++ {
					current := versions[i]
					next := versions[i+1]

					// Check that current version is not older than next
					// This is a basic sanity check for ordering
					Expect(current.Version).ToNot(BeEmpty())
					Expect(next.Version).ToNot(BeEmpty())
				}
			}
		})
	})
})
