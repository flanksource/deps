package github

import (
	"context"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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

				result := filterReleaseCandidates(releases, iterator.stableOnly, nil, 10)
				Expect(result).To(HaveLen(3))
				Expect(result[0].TagName).To(Equal("v1.0.0"))
				Expect(result[1].TagName).To(Equal("v1.1.0-rc1"))
				Expect(result[2].TagName).To(Equal("v0.9.0"))
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

				result := filterReleaseCandidates(releases, iterator.stableOnly, nil, 10)
				Expect(result).To(HaveLen(2))
				Expect(result[0].TagName).To(Equal("v1.0.0"))
				Expect(result[1].TagName).To(Equal("v0.9.0"))
			})

			// nodejs/node published v27.0.0-alpha.0.1.1 without GitHub's prerelease flag.
			It("should filter out a semver prerelease GitHub doesn't flag", func() {
				releases := []restRelease{
					{TagName: "v27.0.0-alpha.0.1.1", Prerelease: false},
					{TagName: "v26.13.1", Prerelease: false},
				}

				result := filterReleaseCandidates(releases, true, nil, 10)
				Expect(result).To(HaveLen(1))
				Expect(result[0].TagName).To(Equal("v26.13.1"))
			})

			// A package with a url_template, like node, resolves stable and latest by listing releases.
			DescribeTable("resolving by version skips a semver prerelease GitHub doesn't flag", func(version string) {
				useGitHubRESTTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
					Expect(req.URL.Path).To(Equal("/repos/nodejs/node/releases"))
					body := `[{"tag_name":"v27.0.0-alpha.0.1.1","prerelease":false},{"tag_name":"v26.13.1","prerelease":false}]`
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				}))
				tag, err := NewGitHubReleaseManager().findReleaseByVersion(context.Background(), "nodejs", "node", version, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(tag).To(Equal("v26.13.1"))
			},
				Entry("stable", "stable"),
				Entry("latest", "latest"),
			)

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

				result := filterReleaseCandidates(releases, iterator.stableOnly, nil, 2)
				Expect(result).To(HaveLen(2))
				Expect(result[0].TagName).To(Equal("v3.0.0"))
				Expect(result[1].TagName).To(Equal("v2.0.0"))
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
