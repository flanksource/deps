package url

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestURL(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "URL Manager Suite")
}

var _ = Describe("URL Manager", func() {
	var (
		manager *URLManager
		ctx     context.Context
	)

	BeforeEach(func() {
		manager = NewURLManager()
		ctx = context.Background()
	})

	Describe("Name", func() {
		It("should return 'url' as manager name", func() {
			Expect(manager.Name()).To(Equal("url"))
		})
	})

	Describe("DiscoverVersions", func() {
		Context("with simple string array", func() {
			It("should parse versions from JSON array", func() {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					versions := []string{"1.0.0", "1.1.0", "2.0.0"}
					json.NewEncoder(w).Encode(versions)
				}))
				defer server.Close()

				pkg := types.Package{
					Name:        "test-package",
					VersionsURL: server.URL,
				}

				versions, err := manager.DiscoverVersions(ctx, pkg, platform.Platform{}, 0)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).To(HaveLen(3))
				Expect(versions[0].Version).To(Equal("2.0.0")) // Should be sorted descending
				Expect(versions[1].Version).To(Equal("1.1.0"))
				Expect(versions[2].Version).To(Equal("1.0.0"))
			})

			It("should respect limit parameter", func() {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					versions := []string{"1.0.0", "1.1.0", "2.0.0", "3.0.0"}
					json.NewEncoder(w).Encode(versions)
				}))
				defer server.Close()

				pkg := types.Package{
					Name:        "test-package",
					VersionsURL: server.URL,
				}

				versions, err := manager.DiscoverVersions(ctx, pkg, platform.Platform{}, 2)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).To(HaveLen(2))
			})
		})

		Context("with version objects", func() {
			It("should parse version objects", func() {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					versions := []map[string]interface{}{
						{"version": "1.0.0", "tag": "v1.0.0"},
						{"version": "2.0.0", "tag": "v2.0.0", "prerelease": true},
					}
					json.NewEncoder(w).Encode(versions)
				}))
				defer server.Close()

				pkg := types.Package{
					Name:        "test-package",
					VersionsURL: server.URL,
				}

				versions, err := manager.DiscoverVersions(ctx, pkg, platform.Platform{}, 0)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).To(HaveLen(2))
				Expect(versions[0].Version).To(Equal("2.0.0"))
				Expect(versions[0].Tag).To(Equal("v2.0.0"))
				Expect(versions[0].Prerelease).To(BeTrue())
			})
		})

		Context("error handling", func() {
			It("should error when versions_url is missing", func() {
				pkg := types.Package{
					Name: "test-package",
				}

				_, err := manager.DiscoverVersions(ctx, pkg, platform.Platform{}, 0)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("versions_url is required"))
			})

			It("should error on HTTP failure", func() {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
				defer server.Close()

				pkg := types.Package{
					Name:        "test-package",
					VersionsURL: server.URL,
				}

				_, err := manager.DiscoverVersions(ctx, pkg, platform.Platform{}, 0)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("HTTP 404"))
			})

			It("should error on invalid JSON", func() {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("invalid json"))
				}))
				defer server.Close()

				pkg := types.Package{
					Name:        "test-package",
					VersionsURL: server.URL,
				}

				_, err := manager.DiscoverVersions(ctx, pkg, platform.Platform{}, 0)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to parse JSON"))
			})
		})
	})

	Describe("Resolve", func() {
		It("should build download URL", func() {
			pkg := types.Package{
				Name:        "test-package",
				URLTemplate: "https://example.com/{{.version}}/package.tar.gz",
			}

			plat := platform.Platform{OS: "linux", Arch: "amd64"}
			resolution, err := manager.Resolve(ctx, pkg, "1.0.0", plat)

			Expect(err).ToNot(HaveOccurred())
			Expect(resolution.DownloadURL).To(Equal("https://example.com/1.0.0/package.tar.gz"))
			Expect(resolution.Version).To(Equal("1.0.0"))
			Expect(resolution.Platform).To(Equal(plat))
		})

		It("should resolve asset patterns", func() {
			pkg := types.Package{
				Name:        "test-package",
				URLTemplate: "https://example.com/{{.asset}}",
				AssetPatterns: map[string]string{
					"linux-amd64": "package-{{.version}}-linux-amd64.tar.gz",
				},
			}

			plat := platform.Platform{OS: "linux", Arch: "amd64"}
			resolution, err := manager.Resolve(ctx, pkg, "1.0.0", plat)

			Expect(err).ToNot(HaveOccurred())
			Expect(resolution.DownloadURL).To(Equal("https://example.com/package-1.0.0-linux-amd64.tar.gz"))
		})

		It("should error when url_template is missing", func() {
			pkg := types.Package{
				Name: "test-package",
			}

			_, err := manager.Resolve(ctx, pkg, "1.0.0", platform.Platform{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("url_template is required"))
		})
	})

	Describe("parseVersions", func() {
		It("should detect prerelease versions", func() {
			data := []interface{}{"1.0.0", "2.0.0-alpha", "3.0.0-beta", "4.0.0-rc1"}
			pkg := types.Package{}

			versions, err := manager.parseVersions(data, pkg)
			Expect(err).ToNot(HaveOccurred())
			Expect(versions).To(HaveLen(4))
			Expect(versions[0].Prerelease).To(BeFalse())
			Expect(versions[1].Prerelease).To(BeTrue())
			Expect(versions[2].Prerelease).To(BeTrue())
			Expect(versions[3].Prerelease).To(BeTrue())
		})
	})
})
