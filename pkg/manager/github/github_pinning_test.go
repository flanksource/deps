package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	pinnedCommit   = "712ce23a27c49bcf4bf161044c8c2203abecadeb"
	otherCommit    = "1111111111111111111111111111111111111111"
	annotatedTagID = "2222222222222222222222222222222222222222"
)

// useGitRefs serves a git-upload-pack ref advertisement with the given "<sha> <ref>" lines.
func useGitRefs(lines ...string) {
	GinkgoHelper()
	previous := gitRefsHTTPClient
	DeferCleanup(func() { gitRefsHTTPClient = previous })

	body := pktLine("# service=git-upload-pack\n") + "0000"
	for i, line := range lines {
		if i == 0 {
			line += "\x00multi_ack thin-pack"
		}
		body += pktLine(line + "\n")
	}
	body += "0000"

	gitRefsHTTPClient = func() *http.Client {
		return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}
	}
}

// useReleases serves GET /repos/owner/plugins/releases/tags/<tag> from the given JSON bodies
// and answers 404 for any other tag.
func useReleases(byTag map[string]string) {
	GinkgoHelper()
	useGitHubRESTTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		tag, isTagLookup := strings.CutPrefix(req.URL.Path, "/repos/owner/plugins/releases/tags/")
		if body, ok := byTag[tag]; ok && isTagLookup {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"message":"Not Found"}`)), Header: make(http.Header)}, nil
	}))
}

func pluginRelease(tag string, immutable bool) string {
	return fmt.Sprintf(`{"tag_name":%q,"immutable":%t,"assets":[
		{"name":"opensearch_%[1]s_linux_amd64.tar.gz","browser_download_url":"https://example.com/opensearch","digest":"sha256:aaaa"},
		{"name":"kubernetes-logs_%[1]s_linux_amd64.tar.gz","browser_download_url":"https://example.com/logs","digest":"sha256:bbbb"}
	]}`, tag, immutable)
}

var _ = Describe("Release pinning", func() {
	pkg := types.Package{Name: "plugins", Repo: "owner/plugins", Manager: "github_release"}
	linux := platform.Platform{OS: "linux", Arch: "amd64"}
	opensearch := func() context.Context {
		return manager.WithAssetFilters(context.Background(), []string{"opensearch"})
	}

	DescribeTable("finds the tags pointing at a commit",
		func(refs []GitRef, expected []string) {
			Expect(tagsPointingAt(refs, pinnedCommit)).To(Equal(expected))
		},
		Entry("lightweight tag", []GitRef{{Name: "refs/tags/v1.8.0", SHA: pinnedCommit}}, []string{"v1.8.0"}),
		Entry("annotated tag by its peeled commit, not the tag object",
			[]GitRef{{Name: "refs/tags/v1.8.0", SHA: annotatedTagID}, {Name: "refs/tags/v1.8.0^{}", SHA: pinnedCommit}},
			[]string{"v1.8.0"}),
		Entry("annotated tag whose object id equals the commit is not a match",
			[]GitRef{{Name: "refs/tags/v1.8.0", SHA: pinnedCommit}, {Name: "refs/tags/v1.8.0^{}", SHA: otherCommit}},
			[]string(nil)),
		Entry("branches are ignored", []GitRef{{Name: "refs/heads/main", SHA: pinnedCommit}}, []string(nil)),
		Entry("newest version first",
			[]GitRef{{Name: "refs/tags/v1.7.1-beta.64", SHA: pinnedCommit}, {Name: "refs/tags/v1.8.0", SHA: pinnedCommit}},
			[]string{"v1.8.0", "v1.7.1-beta.64"}),
	)

	Context("with a commit version", func() {
		It("installs the immutable release whose tag points at the commit", func() {
			useGitRefs(
				otherCommit+" refs/tags/v1.7.0",
				annotatedTagID+" refs/tags/v1.8.0",
				pinnedCommit+" refs/tags/v1.8.0^{}",
			)
			useReleases(map[string]string{"v1.8.0": pluginRelease("v1.8.0", true)})

			resolution, err := NewGitHubReleaseManager().Resolve(opensearch(), pkg, pinnedCommit, linux)
			Expect(err).NotTo(HaveOccurred())
			Expect([]any{resolution.Version, resolution.Checksum, resolution.GitHubAsset}).To(Equal([]any{
				"1.8.0", "sha256:aaaa", &types.GitHubAsset{
					Repo: "owner/plugins", Tag: "v1.8.0", AssetName: "opensearch_v1.8.0_linux_amd64.tar.gz", DownloadURL: "https://example.com/opensearch",
				},
			}))
		})

		It("skips tags at the commit that have no release", func() {
			useGitRefs(pinnedCommit+" refs/tags/v1.9.0-rc.1", pinnedCommit+" refs/tags/v1.8.0")
			useReleases(map[string]string{"v1.8.0": pluginRelease("v1.8.0", true)})

			resolution, err := NewGitHubReleaseManager().Resolve(opensearch(), pkg, pinnedCommit, linux)
			Expect(err).NotTo(HaveOccurred())
			Expect(resolution.GitHubAsset.Tag).To(Equal("v1.8.0"))
		})

		It("requires the release to be immutable", func() {
			useGitRefs(pinnedCommit + " refs/tags/v1.8.0")
			useReleases(map[string]string{"v1.8.0": pluginRelease("v1.8.0", false)})

			_, err := NewGitHubReleaseManager().Resolve(opensearch(), pkg, pinnedCommit, linux)
			Expect(err).To(MatchError(ContainSubstring("release v1.8.0 of owner/plugins is not immutable")))
		})

		It("fails when no tag points at the commit", func() {
			useGitRefs(otherCommit + " refs/tags/v1.8.0")

			_, err := NewGitHubReleaseManager().Resolve(opensearch(), pkg, pinnedCommit, linux)
			Expect(err).To(MatchError(ContainSubstring("no tag in owner/plugins points at commit " + pinnedCommit)))
		})

		It("fails when no tag at the commit has a release", func() {
			useGitRefs(pinnedCommit + " refs/tags/v1.8.0")
			useReleases(map[string]string{})

			_, err := NewGitHubReleaseManager().Resolve(opensearch(), pkg, pinnedCommit, linux)
			Expect(err).To(MatchError(ContainSubstring("no release for tags [v1.8.0] at commit " + pinnedCommit)))
		})
	})

	Context("with +immutable", func() {
		requireImmutable := func() context.Context {
			return manager.WithRequireImmutable(opensearch(), true)
		}

		It("accepts an immutable release", func() {
			useReleases(map[string]string{"v1.8.0": pluginRelease("v1.8.0", true)})

			resolution, err := NewGitHubReleaseManager().Resolve(requireImmutable(), pkg, "v1.8.0", linux)
			Expect(err).NotTo(HaveOccurred())
			Expect(resolution.GitHubAsset.AssetName).To(Equal("opensearch_v1.8.0_linux_amd64.tar.gz"))
		})

		It("rejects a mutable release instead of falling back", func() {
			useReleases(map[string]string{"v1.8.0": pluginRelease("v1.8.0", false)})

			_, err := NewGitHubReleaseManager().Resolve(requireImmutable(), pkg, "v1.8.0", linux)
			Expect(err).To(MatchError(ContainSubstring("release v1.8.0 of owner/plugins is not immutable")))
		})

		It("does not use the no-API path, which cannot see immutability", func() {
			deterministic := types.Package{
				Name: "plugins", Repo: "owner/plugins", Manager: "github_release",
				ChecksumFile:  "{{.tag}}_checksums.txt",
				AssetPatterns: map[string]string{"linux-amd64": "opensearch_{{.tag}}_linux_amd64.tar.gz"},
			}
			useReleases(map[string]string{"v1.8.0": pluginRelease("v1.8.0", false)})

			_, err := NewGitHubReleaseManager().Resolve(manager.WithRequireImmutable(context.Background(), true), deterministic, "v1.8.0", linux)
			Expect(err).To(MatchError(ContainSubstring("not immutable")))
		})
	})
})
