package github

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func useGitHubRESTTransport(transport roundTripFunc) {
	GinkgoHelper()
	client := GetClient()
	client.mu.Lock()
	previousHTTPClient := client.httpClient
	client.httpClient = &http.Client{Transport: transport}
	client.mu.Unlock()
	DeferCleanup(func() {
		client.mu.Lock()
		client.httpClient = previousHTTPClient
		client.mu.Unlock()
	})
}

var _ = Describe("GitHub install filters", func() {
	DescribeTable("matches releases by tag or title",
		func(release restRelease, filters []string, expected bool) {
			Expect(releaseMatchesFilters(release, filters)).To(Equal(expected))
		},
		Entry("without filters", restRelease{TagName: "v1.2.3", Name: "CLI release"}, nil, true),
		Entry("by exact tag", restRelease{TagName: "v1.2.3", Name: "CLI release"}, []string{"v1.2.3"}, true),
		Entry("by title wildcard", restRelease{TagName: "v1.2.3", Name: "CLI release"}, []string{"cli*"}, true),
		Entry("case insensitively", restRelease{TagName: "v1.2.3", Name: "CLI release"}, []string{"*RELEASE"}, true),
		Entry("with no positive match", restRelease{TagName: "v1.2.3", Name: "CLI release"}, []string{"server*"}, false),
		Entry("with a tag exclusion", restRelease{TagName: "v1.2.3", Name: "CLI release"}, []string{"cli*", "!v1*"}, false),
		Entry("with a title exclusion", restRelease{TagName: "v1.2.3", Name: "CLI beta"}, []string{"v1*", "!*beta*"}, false),
		Entry("with exclusions only", restRelease{TagName: "v1.2.3", Name: "CLI release"}, []string{"!server*"}, true),
	)

	Describe("asset selection", func() {
		assets := []restAsset{
			{Name: "tool-linux-amd64.tar.gz"},
			{Name: "tool-darwin-arm64.tar.gz"},
			{Name: "tool"},
		}

		It("prefers an exact match before retrying as a prefix", func() {
			Expect(selectReleaseAsset(assets, []string{"tool"})).To(Equal(&assets[2]))
		})

		It("appends a wildcard when the exact filter has no match", func() {
			Expect(selectReleaseAsset(assets, []string{"tool-linux"})).To(Equal(&assets[0]))
		})

		It("supports MatchItems wildcards and exclusions", func() {
			filters := []string{"tool-*", "!*darwin*"}
			Expect(selectReleaseAsset(assets, filters)).To(Equal(&assets[0]))
		})

		It("returns nil when neither matching pass finds an asset", func() {
			Expect(selectReleaseAsset(assets, []string{"server"})).To(BeNil())
		})
	})

	Describe("release candidate filtering", func() {
		It("filters drafts, prereleases, and MatchItems patterns before applying the limit", func() {
			releases := []restRelease{
				{TagName: "v4.0.0", Name: "Server stable", Draft: true},
				{TagName: "v3.0.0-beta.1", Name: "CLI beta", Prerelease: true},
				{TagName: "v2.1.0", Name: "Server stable"},
				{TagName: "v2.0.0", Name: "CLI stable"},
				{TagName: "v1.0.0", Name: "CLI stable"},
			}

			candidates := filterReleaseCandidates(releases, true, []string{"cli*"}, 1)
			Expect(candidates).To(Equal([]restRelease{releases[3]}))
		})
	})

	Describe("filtered resolution", func() {
		It("builds a resolution from the explicitly selected asset", func() {
			ctx := manager.WithAssetFilters(context.Background(), []string{"cli-linux"})
			release := &restRelease{
				TagName: "v2.0.0",
				Assets: []restAsset{
					{Name: "server-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/server", Digest: "sha256:server"},
					{Name: "cli-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/cli", Digest: "sha256:cli"},
				},
			}

			resolution, err := NewGitHubReleaseManager().buildResolutionFromRelease(ctx, types.Package{Name: "tool", Repo: "owner/tool"}, release, platform.Platform{OS: "linux", Arch: "amd64"})
			Expect(err).NotTo(HaveOccurred())
			actual := struct {
				DownloadURL string
				Checksum    string
				GitHubAsset *types.GitHubAsset
			}{resolution.DownloadURL, resolution.Checksum, resolution.GitHubAsset}
			Expect(actual).To(Equal(struct {
				DownloadURL string
				Checksum    string
				GitHubAsset *types.GitHubAsset
			}{"https://example.com/cli", "sha256:cli", &types.GitHubAsset{
				Repo: "owner/tool", Tag: "v2.0.0", AssetName: "cli-linux-amd64.tar.gz", DownloadURL: "https://example.com/cli",
			}}))
		})

		It("selects the newest stable release matching its tag or title", func() {
			useGitHubRESTTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				Expect(req.URL.Path).To(Equal("/repos/owner/tool/releases"))
				body := `[
					{"tag_name":"v3.0.0","name":"Server release","assets":[{"name":"server-linux.tar.gz","browser_download_url":"https://example.com/server"}]},
					{"tag_name":"v2.0.0-beta.1","name":"CLI beta","prerelease":true,"assets":[{"name":"cli-linux.tar.gz","browser_download_url":"https://example.com/beta"}]},
					{"tag_name":"v1.0.0","name":"CLI release","assets":[{"name":"cli-linux.tar.gz","browser_download_url":"https://example.com/cli"}]}
				]`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			}))

			ctx := manager.WithReleaseFilters(context.Background(), []string{"cli*"})
			ctx = manager.WithAssetFilters(ctx, []string{"cli-linux"})
			resolution, err := NewGitHubReleaseManager().Resolve(ctx, types.Package{Name: "tool", Repo: "owner/tool", Manager: "github_release"}, "latest", platform.Platform{OS: "linux", Arch: "amd64"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resolution.GitHubAsset).To(Equal(&types.GitHubAsset{
				Repo: "owner/tool", Tag: "v1.0.0", AssetName: "cli-linux.tar.gz", DownloadURL: "https://example.com/cli",
			}))
		})

		It("filters version discovery by release title", func() {
			useGitHubRESTTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				Expect(req.URL.Path).To(Equal("/repos/owner/tool/releases"))
				body := `[
					{"tag_name":"v2.0.0","name":"Server release"},
					{"tag_name":"v1.0.0","name":"CLI release"}
				]`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			}))

			ctx := manager.WithReleaseFilters(context.Background(), []string{"cli*"})
			versions, err := NewGitHubReleaseManager().DiscoverVersions(ctx, types.Package{Name: "tool", Repo: "owner/tool"}, platform.Platform{}, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(versions).To(HaveLen(1))
			Expect(versions[0].Tag).To(Equal("v1.0.0"))
		})

		It("rejects an explicit version excluded by the release filter", func() {
			useGitHubRESTTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				Expect(req.URL.Path).To(Equal("/repos/owner/tool/releases/tags/v2.0.0"))
				body := `{"tag_name":"v2.0.0","name":"Server release"}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			}))

			ctx := manager.WithReleaseFilters(context.Background(), []string{"cli*"})
			_, err := NewGitHubReleaseManager().Resolve(ctx, types.Package{Name: "tool", Repo: "owner/tool", Manager: "github_release"}, "v2.0.0", platform.Platform{OS: "linux", Arch: "amd64"})
			Expect(err).To(MatchError("v2.0.0 not found"))
		})
	})
})
