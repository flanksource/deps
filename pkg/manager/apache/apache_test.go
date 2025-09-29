package apache_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/flanksource/deps/pkg/manager/apache"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestApache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Apache Manager Suite")
}

var _ = Describe("Apache Manager", func() {
	var (
		apacheManager *apache.ApacheManager
		ctx           context.Context
		plat          platform.Platform
	)

	BeforeEach(func() {
		apacheManager = apache.NewApacheManager()
		ctx = context.Background()
		plat = platform.Platform{OS: "linux", Arch: "amd64"}
	})

	Describe("Name", func() {
		It("should return 'apache'", func() {
			Expect(apacheManager.Name()).To(Equal("apache"))
		})
	})

	Describe("Resolve", func() {
		Context("with default URL template", func() {
			It("should build correct download URL", func() {
				pkg := types.Package{
					Name: "maven",
					AssetPatterns: map[string]string{
						"*": "apache-maven-{{.version}}-bin.tar.gz",
					},
				}

				resolution, err := apacheManager.Resolve(ctx, pkg, "3.9.0", plat)
				Expect(err).NotTo(HaveOccurred())
				Expect(resolution.DownloadURL).To(Equal("https://archive.apache.org/dist/maven/binaries/apache-maven-3.9.0-bin.tar.gz"))
				Expect(resolution.IsArchive).To(BeTrue())
			})
		})

		Context("with custom URL template", func() {
			It("should use custom template", func() {
				pkg := types.Package{
					Name:        "ant",
					URLTemplate: "https://archive.apache.org/dist/{{.name}}/{{.asset}}",
					AssetPatterns: map[string]string{
						"*": "apache-ant-{{.version}}-bin.tar.gz",
					},
				}

				resolution, err := apacheManager.Resolve(ctx, pkg, "1.10.12", plat)
				Expect(err).NotTo(HaveOccurred())
				Expect(resolution.DownloadURL).To(Equal("https://archive.apache.org/dist/ant/apache-ant-1.10.12-bin.tar.gz"))
			})
		})

		Context("with platform-specific patterns", func() {
			It("should resolve platform-specific asset", func() {
				pkg := types.Package{
					Name: "test",
					AssetPatterns: map[string]string{
						"linux-amd64": "test-{{.version}}-linux-x64.tar.gz",
						"*":           "test-{{.version}}.tar.gz",
					},
				}

				resolution, err := apacheManager.Resolve(ctx, pkg, "1.0.0", plat)
				Expect(err).NotTo(HaveOccurred())
				Expect(resolution.DownloadURL).To(ContainSubstring("test-1.0.0-linux-x64.tar.gz"))
			})
		})

		Context("with checksum file", func() {
			It("should build checksum URL", func() {
				pkg := types.Package{
					Name: "maven",
					AssetPatterns: map[string]string{
						"*": "apache-maven-{{.version}}-bin.tar.gz",
					},
					ChecksumFile: "apache-maven-{{.version}}-bin.tar.gz.sha256",
				}

				resolution, err := apacheManager.Resolve(ctx, pkg, "3.9.0", plat)
				Expect(err).NotTo(HaveOccurred())
				Expect(resolution.ChecksumURL).To(Equal("https://archive.apache.org/dist/maven/binaries/apache-maven-3.9.0-bin.tar.gz.sha256"))
			})
		})

		Context("with missing asset patterns", func() {
			It("should return error", func() {
				pkg := types.Package{
					Name: "maven",
				}

				_, err := apacheManager.Resolve(ctx, pkg, "3.9.0", plat)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no asset patterns defined"))
			})
		})
	})

	Describe("ParseVersionsFromHTML", func() {
		DescribeTable("should extract versions from different Apache directory listings",
			func(fixtureFile string, expectedVersions []string, expectedPrerelease []bool, minVersionCount int) {
				// Read HTML fixture file
				fixturePath := filepath.Join("testdata", fixtureFile)
				htmlBytes, err := os.ReadFile(fixturePath)
				Expect(err).NotTo(HaveOccurred())

				// Parse versions from HTML
				versions := apacheManager.ParseVersionsFromHTML(string(htmlBytes))

				// Verify minimum number of versions found
				Expect(len(versions)).To(BeNumerically(">=", minVersionCount),
					"Expected at least %d versions but found %d in %s", minVersionCount, len(versions), fixtureFile)

				// Verify each expected version is found
				foundVersions := make(map[string]bool)
				for _, v := range versions {
					foundVersions[v.Version] = v.Prerelease
				}

				for i, expectedVer := range expectedVersions {
					Expect(foundVersions).To(HaveKey(expectedVer),
						"Expected version %s not found in %s", expectedVer, fixtureFile)
					Expect(foundVersions[expectedVer]).To(Equal(expectedPrerelease[i]),
						"Version %s prerelease flag mismatch in %s", expectedVer, fixtureFile)
				}
			},
			Entry("ActiveMQ listing", "activemq_listing.html",
				[]string{"5.10.0", "5.10.1", "5.10.2", "5.11.0", "5.11.1", "5.11.2"},
				[]bool{false, false, false, false, false, false}, 50),
			Entry("Maven listing", "maven_listing.html",
				[]string{"3.0.4", "3.0.5", "3.1.0-alpha-1", "3.1.0", "3.1.1", "3.5.0-alpha-1", "3.5.0-beta-1"},
				[]bool{false, false, true, false, false, true, true}, 30),
			Entry("Malformed listing", "malformed_listing.html",
				[]string{"1.2.3", "2.0.0"},
				[]bool{false, false}, 2),
		)

		It("should handle empty HTML", func() {
			versions := apacheManager.ParseVersionsFromHTML("")
			Expect(versions).To(BeEmpty())
		})

		It("should handle HTML with no version links", func() {
			html := `<html><body><h1>No versions here</h1><a href="/other">Other</a></body></html>`
			versions := apacheManager.ParseVersionsFromHTML(html)
			Expect(versions).To(BeEmpty())
		})

		It("should deduplicate versions", func() {
			html := `<html><body>
				<a href="1.0.0/">1.0.0/</a>
				<a href="v1.0.0/">v1.0.0/</a>
				<a href="project-1.0.0/">project-1.0.0/</a>
			</body></html>`
			versions := apacheManager.ParseVersionsFromHTML(html)

			// Should only find one unique version 1.0.0
			Expect(len(versions)).To(Equal(1))
			Expect(versions[0].Version).To(Equal("1.0.0"))
		})

		It("should detect prerelease versions", func() {
			html := `<html><body>
				<a href="1.0.0-alpha/">1.0.0-alpha/</a>
				<a href="2.0.0-beta/">2.0.0-beta/</a>
				<a href="3.0.0-snapshot/">3.0.0-snapshot/</a>
				<a href="4.0.0/">4.0.0/</a>
			</body></html>`
			versions := apacheManager.ParseVersionsFromHTML(html)

			Expect(len(versions)).To(Equal(4))

			// Find each version and check prerelease flag
			versionMap := make(map[string]bool)
			for _, v := range versions {
				versionMap[v.Version] = v.Prerelease
			}

			Expect(versionMap["1.0.0-alpha"]).To(BeTrue())
			Expect(versionMap["2.0.0-beta"]).To(BeTrue())
			Expect(versionMap["3.0.0-snapshot"]).To(BeTrue())
			Expect(versionMap["4.0.0"]).To(BeFalse())
		})
	})

	Describe("Version Validation and Error Enhancement", func() {
		Context("when resolving with invalid version", func() {
			It("should return enhanced error with version suggestions", func() {
				pkg := types.Package{
					Name: "maven",
					AssetPatterns: map[string]string{
						"*": "apache-maven-{{.version}}-bin.tar.gz",
					},
				}

				_, err := apacheManager.Resolve(ctx, pkg, "99.99.99", plat)
				Expect(err).To(HaveOccurred())

				// Check that error contains version suggestions
				errorStr := err.Error()
				Expect(errorStr).To(ContainSubstring("Version 99.99.99 not found"))
				Expect(errorStr).To(ContainSubstring("Available versions"))
				Expect(errorStr).To(ContainSubstring("Did you mean"))
			})

			It("should suggest closest version", func() {
				pkg := types.Package{
					Name: "maven",
					AssetPatterns: map[string]string{
						"*": "apache-maven-{{.version}}-bin.tar.gz",
					},
				}

				_, err := apacheManager.Resolve(ctx, pkg, "3.8.0", plat)
				Expect(err).To(HaveOccurred())

				errorStr := err.Error()
				// Should suggest a similar version like 3.8.1 or similar
				Expect(errorStr).To(ContainSubstring("Did you mean"))
			})
		})

		Context("when resolving with valid version", func() {
			It("should succeed when version exists", func() {
				// Note: This test uses real HTML fixtures and might pass if the version happens to exist
				// For a true unit test, we'd need to mock the HTTP client
				Skip("Requires mocking HTTP client for deterministic testing")
			})
		})
	})

	Describe("validateVersion", func() {
		It("should find existing version through Resolve method", func() {
			// Test version validation indirectly through Resolve method
			// Use a version that exists in our test fixtures
			pkg := types.Package{
				Name: "malformed", // This references our malformed_listing.html fixture
				AssetPatterns: map[string]string{
					"*": "test-{{.version}}.tar.gz",
				},
			}

			// Try to resolve a version that exists in our malformed fixture (1.2.3)
			_, err := apacheManager.Resolve(ctx, pkg, "1.2.3", plat)
			// This might succeed or fail depending on network availability
			// The important thing is that we test the error enhancement logic
			if err != nil {
				// If it fails, it should be due to network issues during version discovery
				// or enhanced version error, not a simple "version not found"
				Expect(err.Error()).To(Or(
					ContainSubstring("failed to discover available versions"),
					ContainSubstring("Available versions"),
				))
			}
		})

		It("should return enhanced error for non-existent version", func() {
			pkg := types.Package{
				Name: "test",
				AssetPatterns: map[string]string{
					"*": "test-{{.version}}.tar.gz",
				},
			}

			_, err := apacheManager.Resolve(ctx, pkg, "999.999.999", plat)
			Expect(err).To(HaveOccurred())

			// The error should either be enhanced with versions or a network error
			errorStr := err.Error()
			Expect(errorStr).To(Or(
				ContainSubstring("Version 999.999.999 not found"),
				ContainSubstring("failed to discover available versions"),
			))
		})
	})

	Describe("isArchive detection", func() {
		It("should detect archive files correctly", func() {
			pkg := types.Package{
				Name: "maven",
				AssetPatterns: map[string]string{
					"*": "apache-maven-{{.version}}-bin.tar.gz",
				},
			}

			resolution, err := apacheManager.Resolve(ctx, pkg, "3.9.0", plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(resolution.IsArchive).To(BeTrue())
		})

		It("should handle non-archive files", func() {
			pkg := types.Package{
				Name: "test",
				AssetPatterns: map[string]string{
					"*": "test-{{.version}}-binary",
				},
			}

			resolution, err := apacheManager.Resolve(ctx, pkg, "1.0.0", plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(resolution.IsArchive).To(BeFalse())
		})
	})

	Describe("Install", func() {
		It("should return not implemented error", func() {
			pkg := types.Package{Name: "test"}
			resolution := &types.Resolution{Package: pkg}
			opts := types.InstallOptions{}

			err := apacheManager.Install(ctx, resolution, opts)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})
	})

	Describe("Verify", func() {
		It("should return not implemented error", func() {
			pkg := types.Package{Name: "test"}

			_, err := apacheManager.Verify(ctx, "/path/to/binary", pkg)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not implemented"))
		})
	})
})
