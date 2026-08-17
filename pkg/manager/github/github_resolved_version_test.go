package github

import (
	"context"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("resolved version reporting", func() {
	// A url_template package with no asset filters templates its download URL locally, so
	// resolveViaGoGitHub returns without touching the REST API.
	pkg := types.Package{
		Name:          "helm-like",
		Repo:          "owner/helm-like",
		URLTemplate:   "https://get.example.com/{{.asset}}",
		AssetPatterns: map[string]string{"linux-amd64": "helm-like-{{.tag}}-linux-amd64.tar.gz"},
	}
	plat := platform.Platform{OS: "linux", Arch: "amd64"}

	It("reports the tag behind an alias rather than the alias", func() {
		res, err := NewGitHubReleaseManager().resolveViaGoGitHub(context.Background(), pkg, "latest", "v3.52.0", plat)

		Expect(err).ToNot(HaveOccurred())
		Expect(res.Version).To(Equal("3.52.0"))
		Expect(res.DownloadURL).To(Equal("https://get.example.com/helm-like-v3.52.0-linux-amd64.tar.gz"))
	})

	It("reports a pinned version unchanged", func() {
		res, err := NewGitHubReleaseManager().resolveViaGoGitHub(context.Background(), pkg, "3.52.0", "v3.52.0", plat)

		Expect(err).ToNot(HaveOccurred())
		Expect(res.Version).To(Equal("3.52.0"))
	})
})
