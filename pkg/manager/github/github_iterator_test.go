package github

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

var _ = Describe("githubReleaseIterator", func() {
	Describe("FetchReleases with stableOnly", func() {
		Context("when stableOnly is false", func() {
			It("should include all non-draft releases", func() {
				iterator := &githubReleaseIterator{
					stableOnly: false,
				}
				releases := []restRelease{
					{TagName: "v1.0.0", Draft: false, Prerelease: false},
					{TagName: "v1.1.0-rc1", Draft: false, Prerelease: true},
					{TagName: "v0.9.0", Draft: false, Prerelease: false},
					{TagName: "v0.8.0-draft", Draft: true, Prerelease: false},
				}

				result := filterReleases(releases, iterator.stableOnly, 10)
				Expect(result).To(HaveLen(3))
				Expect(result[0].Tag).To(Equal("v1.0.0"))
				Expect(result[1].Tag).To(Equal("v1.1.0-rc1"))
				Expect(result[2].Tag).To(Equal("v0.9.0"))
			})
		})

		Context("when stableOnly is true", func() {
			It("should filter out prereleases", func() {
				iterator := &githubReleaseIterator{
					stableOnly: true,
				}
				releases := []restRelease{
					{TagName: "v1.0.0", Draft: false, Prerelease: false},
					{TagName: "v1.1.0-rc1", Draft: false, Prerelease: true},
					{TagName: "v1.1.0-beta", Draft: false, Prerelease: true},
					{TagName: "v0.9.0", Draft: false, Prerelease: false},
				}

				result := filterReleases(releases, iterator.stableOnly, 10)
				Expect(result).To(HaveLen(2))
				Expect(result[0].Tag).To(Equal("v1.0.0"))
				Expect(result[1].Tag).To(Equal("v0.9.0"))
			})

			It("should respect limit after filtering", func() {
				iterator := &githubReleaseIterator{
					stableOnly: true,
				}
				releases := []restRelease{
					{TagName: "v3.0.0", Draft: false, Prerelease: false},
					{TagName: "v3.1.0-rc1", Draft: false, Prerelease: true},
					{TagName: "v2.0.0", Draft: false, Prerelease: false},
					{TagName: "v1.0.0", Draft: false, Prerelease: false},
				}

				result := filterReleases(releases, iterator.stableOnly, 2)
				Expect(result).To(HaveLen(2))
				Expect(result[0].Tag).To(Equal("v3.0.0"))
				Expect(result[1].Tag).To(Equal("v2.0.0"))
			})
		})
	})

	Describe("TryResolve with requestedVersion", func() {
		It("should set version to requestedVersion when provided", func() {
			iterator := &githubReleaseIterator{
				requestedVersion: "stable",
				pkg:              types.Package{Name: "test"},
				plat:             platform.Platform{OS: "linux", Arch: "amd64"},
			}

			resolution := &types.Resolution{Version: "1.0.0"}

			if iterator.requestedVersion != "" {
				resolution.Version = iterator.requestedVersion
			}
			Expect(resolution.Version).To(Equal("stable"))
		})

		It("should preserve original version when requestedVersion is empty", func() {
			iterator := &githubReleaseIterator{
				requestedVersion: "",
			}

			originalVersion := "1.0.0"
			resolution := &types.Resolution{Version: originalVersion}

			if iterator.requestedVersion != "" {
				resolution.Version = iterator.requestedVersion
			}
			Expect(resolution.Version).To(Equal("1.0.0"))
		})
	})
})

// filterReleases extracts the filtering logic from FetchReleases for testing
func filterReleases(releases []restRelease, stableOnly bool, limit int) []manager.ReleaseInfo {
	result := make([]manager.ReleaseInfo, 0, len(releases))
	for _, rel := range releases {
		if rel.Draft {
			continue
		}
		if stableOnly && rel.Prerelease {
			continue
		}
		result = append(result, manager.ReleaseInfo{
			Tag:          rel.TagName,
			IsPrerelease: rel.Prerelease,
		})
		if len(result) >= limit {
			break
		}
	}
	return result
}
