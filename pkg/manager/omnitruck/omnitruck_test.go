package omnitruck_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/manager/omnitruck"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOmnitruck(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Omnitruck Manager Suite")
}

const (
	product     = "cinc-auditor"
	channel     = "stable"
	newest      = "7.1.7"
	middle      = "6.6.0"
	oldest      = "4.17.7"
	artifactSHA = "a5a2a9823cee380ff423c1c15058c01977301cae1c48697bdfd3eb61f56402bc"
)

// server stands in for an Omnitruck host, recording what it was asked so the
// platform triple and channel routing can be asserted rather than inferred.
type server struct {
	*httptest.Server

	// LastQuery is the metadata request's parameters.
	LastQuery url.Values
	// LastPath is the path of the most recent request.
	LastPath string

	// Versions is what /versions/all answers.
	Versions []string
	// Metadata is what /metadata answers. A nil value answers `{}`, which is
	// how Omnitruck reports a version it does not have.
	Metadata map[string]string
}

func newServer() *server {
	s := &server{
		Versions: []string{oldest, middle, newest},
		Metadata: map[string]string{
			"sha256":  artifactSHA,
			"url":     "https://packages.example.com/" + product + "_" + newest + "-1_amd64.deb",
			"version": newest,
		},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.LastPath = r.URL.Path
		s.LastQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case fmt.Sprintf("/%s/%s/versions/all", channel, product):
			_ = json.NewEncoder(w).Encode(s.Versions)
		case fmt.Sprintf("/%s/%s/metadata", channel, product):
			if s.Metadata == nil {
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_ = json.NewEncoder(w).Encode(s.Metadata)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return s
}

var _ = Describe("Omnitruck Manager", func() {
	var (
		subject *omnitruck.Manager
		host    *server
		pkg     types.Package
		ctx     context.Context
	)

	BeforeEach(func() {
		subject = omnitruck.New()
		host = newServer()
		DeferCleanup(host.Close)
		ctx = context.Background()
		pkg = types.Package{
			Name:  product,
			Extra: map[string]interface{}{"base_url": host.URL, "channel": channel, "product": product},
		}
	})

	Describe("Name", func() {
		It("identifies itself as omnitruck", func() {
			Expect(subject.Name()).To(Equal("omnitruck"))
		})
	})

	Describe("DiscoverVersions", func() {
		It("returns every published version, newest first", func() {
			versions, err := subject.DiscoverVersions(ctx, pkg, platform.Platform{OS: "linux", Arch: "amd64"}, 0)

			Expect(err).ToNot(HaveOccurred())
			Expect(versions).To(HaveLen(len(host.Versions)))
			Expect(versions[0].Version).To(Equal(newest))
			Expect(versions[len(versions)-1].Version).To(Equal(oldest))
		})

		It("honours a limit by keeping the newest", func() {
			const keep = 2
			versions, err := subject.DiscoverVersions(ctx, pkg, platform.Platform{OS: "linux", Arch: "amd64"}, keep)

			Expect(err).ToNot(HaveOccurred())
			Expect(versions).To(HaveLen(keep))
			Expect(versions[0].Version).To(Equal(newest))
			Expect(versions[1].Version).To(Equal(middle))
		})

		It("reports a server error rather than an empty list", func() {
			host.Close()

			_, err := subject.DiscoverVersions(ctx, pkg, platform.Platform{OS: "linux", Arch: "amd64"}, 0)

			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Resolve", func() {
		DescribeTable("maps a deps platform onto Omnitruck's triple",
			func(plat platform.Platform, expected map[string]string) {
				_, err := subject.Resolve(ctx, pkg, newest, plat)

				Expect(err).ToNot(HaveOccurred())
				for key, value := range expected {
					Expect(host.LastQuery.Get(key)).To(Equal(value), "query parameter %s", key)
				}
			},
			// Linux resolves to the Ubuntu build for every distribution: an
			// omnibus package carries its own interpreter and libraries, so the
			// distribution in the artifact name selects a packaging format.
			Entry("linux amd64", platform.Platform{OS: "linux", Arch: "amd64"},
				map[string]string{"p": "ubuntu", "pv": "22.04", "m": "x86_64"}),
			Entry("linux arm64", platform.Platform{OS: "linux", Arch: "arm64"},
				map[string]string{"p": "ubuntu", "pv": "22.04", "m": "aarch64"}),
			Entry("darwin amd64", platform.Platform{OS: "darwin", Arch: "amd64"},
				map[string]string{"p": "mac_os_x", "pv": "14", "m": "x86_64"}),
			Entry("darwin arm64", platform.Platform{OS: "darwin", Arch: "arm64"},
				map[string]string{"p": "mac_os_x", "pv": "14", "m": "arm64"}),
		)

		It("carries the checksum Omnitruck returned alongside the URL", func() {
			resolution, err := subject.Resolve(ctx, pkg, newest, platform.Platform{OS: "linux", Arch: "amd64"})

			Expect(err).ToNot(HaveOccurred())
			Expect(resolution.DownloadURL).To(Equal(host.Metadata["url"]))
			Expect(resolution.Checksum).To(Equal("sha256:" + artifactSHA))
			Expect(resolution.Version).To(Equal(newest))
		})

		It("never reports an omnibus package as an archive", func() {
			// It is an operating-system package that only works at its build
			// prefix; unpacking it into bin-dir yields a tree that cannot run.
			resolution, err := subject.Resolve(ctx, pkg, newest, platform.Platform{OS: "linux", Arch: "amd64"})

			Expect(err).ToNot(HaveOccurred())
			Expect(resolution.IsArchive).To(BeFalse())
		})

		DescribeTable("spells the requested version the way Omnitruck expects",
			func(requested, expected string) {
				_, err := subject.Resolve(ctx, pkg, requested, platform.Platform{OS: "linux", Arch: "amd64"})

				Expect(err).ToNot(HaveOccurred())
				Expect(host.LastQuery.Get("v")).To(Equal(expected))
			},
			Entry("an exact version passes through", newest, newest),
			Entry("a v-prefixed tag is stripped", "v"+newest, newest),
			Entry("an empty version means newest", "", "latest"),
			Entry("latest means newest", "latest", "latest"),
		)

		It("reports a version the channel does not carry as not found", func() {
			// Omnitruck answers 200 with an empty body for an unknown version,
			// so an absent URL is the only signal it does not exist.
			host.Metadata = nil

			_, err := subject.Resolve(ctx, pkg, "0.0.1", platform.Platform{OS: "linux", Arch: "amd64"})

			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(&manager.ErrVersionNotFound{}))
		})

		It("refuses an artifact with no checksum", func() {
			delete(host.Metadata, "sha256")

			_, err := subject.Resolve(ctx, pkg, newest, platform.Platform{OS: "linux", Arch: "amd64"})

			Expect(err).To(MatchError(ContainSubstring("cannot be verified")))
		})

		It("names the platforms it does support when asked for one it does not", func() {
			_, err := subject.Resolve(ctx, pkg, newest, platform.Platform{OS: "windows", Arch: "amd64"})

			Expect(err).To(MatchError(ContainSubstring("windows-amd64")))
			Expect(err).To(MatchError(ContainSubstring("linux-amd64")))
		})
	})

	Describe("configuration", func() {
		It("defaults the product to the package name", func() {
			pkg.Extra = map[string]interface{}{"base_url": host.URL}

			_, err := subject.Resolve(ctx, pkg, newest, platform.Platform{OS: "linux", Arch: "amd64"})

			Expect(err).ToNot(HaveOccurred())
			Expect(host.LastPath).To(Equal("/" + channel + "/" + product + "/metadata"))
		})

		It("routes to the configured channel", func() {
			// Reconfigure both sides: the fixture only answers for the channel
			// it was built with, so a mismatch would 404 rather than pass.
			const current = "current"
			host.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				host.LastPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(host.Metadata)
			})
			pkg.Extra["channel"] = current

			_, err := subject.Resolve(ctx, pkg, newest, platform.Platform{OS: "linux", Arch: "amd64"})

			Expect(err).ToNot(HaveOccurred())
			Expect(host.LastPath).To(Equal("/" + current + "/" + product + "/metadata"))
		})
	})
})
