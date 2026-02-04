package manager

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/platform"
)

var _ = Describe("Asset Resolution", func() {
	Describe("ResolveAssetPattern", func() {
		var plat platform.Platform

		BeforeEach(func() {
			plat = platform.Platform{OS: "darwin", Arch: "arm64"}
		})

		Context("exact platform match", func() {
			It("should match exact platform key first", func() {
				patterns := map[string]string{
					"darwin-arm64": "exact-match.tar.gz",
					"darwin-*":     "wildcard-match.tar.gz",
					"*":            "all-platforms.tar.gz",
				}

				result, err := ResolveAssetPattern(patterns, plat)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("exact-match.tar.gz"))
			})
		})

		Context("literal * wildcard", func() {
			It("should fall back to * when no exact match", func() {
				patterns := map[string]string{
					"linux-amd64": "linux-specific.tar.gz",
					"*":           "all-platforms.tar.gz",
				}

				result, err := ResolveAssetPattern(patterns, plat)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("all-platforms.tar.gz"))
			})

			It("should work for all platforms with * wildcard", func() {
				patterns := map[string]string{
					"*": "universal-{{.os}}-{{.arch}}.tar.gz",
				}

				platforms := []platform.Platform{
					{OS: "darwin", Arch: "amd64"},
					{OS: "darwin", Arch: "arm64"},
					{OS: "linux", Arch: "amd64"},
					{OS: "linux", Arch: "arm64"},
					{OS: "windows", Arch: "amd64"},
				}

				for _, p := range platforms {
					result, err := ResolveAssetPattern(patterns, p)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal("universal-{{.os}}-{{.arch}}.tar.gz"))
				}
			})
		})

		Context("glob patterns", func() {
			It("should match OS wildcards like darwin-*", func() {
				patterns := map[string]string{
					"darwin-*": "darwin-any.tar.gz",
					"*":        "fallback.tar.gz",
				}

				result, err := ResolveAssetPattern(patterns, plat)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("darwin-any.tar.gz"))
			})

			It("should match linux-* for linux platforms", func() {
				patterns := map[string]string{
					"linux-*": "linux-any.tar.gz",
				}

				linuxPlat := platform.Platform{OS: "linux", Arch: "amd64"}
				result, err := ResolveAssetPattern(patterns, linuxPlat)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("linux-any.tar.gz"))
			})
		})

		Context("comma-separated patterns", func() {
			It("should match comma-separated patterns", func() {
				patterns := map[string]string{
					"darwin-*,windows-*": "desktop-os.zip",
					"linux-*":            "server-os.tar.gz",
				}

				darwinPlat := platform.Platform{OS: "darwin", Arch: "amd64"}
				result, err := ResolveAssetPattern(patterns, darwinPlat)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("desktop-os.zip"))

				windowsPlat := platform.Platform{OS: "windows", Arch: "amd64"}
				result, err = ResolveAssetPattern(patterns, windowsPlat)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("desktop-os.zip"))

				linuxPlat := platform.Platform{OS: "linux", Arch: "amd64"}
				result, err = ResolveAssetPattern(patterns, linuxPlat)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("server-os.tar.gz"))
			})
		})

		Context("priority order", func() {
			It("should prioritize exact match over wildcard", func() {
				patterns := map[string]string{
					"darwin-arm64": "exact.tar.gz",
					"darwin-*":     "os-wildcard.tar.gz",
					"*":            "all.tar.gz",
				}

				result, err := ResolveAssetPattern(patterns, plat)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("exact.tar.gz"))
			})

			It("should prioritize glob patterns over literal *", func() {
				patterns := map[string]string{
					"linux-*":  "linux.tar.gz",
					"darwin-*": "darwin.tar.gz",
					"*":        "universal.tar.gz",
				}

				result, err := ResolveAssetPattern(patterns, plat)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal("darwin.tar.gz")) // Should match darwin-* not *
			})
		})

		Context("error cases", func() {
			It("should return error when no patterns defined", func() {
				patterns := map[string]string{}

				_, err := ResolveAssetPattern(patterns, plat)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no asset patterns defined"))
			})

			It("should return error when no pattern matches", func() {
				patterns := map[string]string{
					"linux-amd64": "linux.tar.gz",
					"windows-*":   "windows.zip",
				}

				_, err := ResolveAssetPattern(patterns, plat, "test-package")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("platform darwin-arm64 not supported"))
				Expect(err.Error()).To(ContainSubstring("available platforms"))
				Expect(err.Error()).To(ContainSubstring("linux-amd64"))

				// Verify it's the correct error type
				var platformErr *ErrPlatformNotSupported
				Expect(errors.As(err, &platformErr)).To(BeTrue())
				Expect(platformErr.Platform).To(Equal("darwin-arm64"))
				Expect(platformErr.Package).To(Equal("test-package"))
				Expect(platformErr.AvailablePlatforms).To(ContainElements("linux-amd64", "windows-*"))
			})
		})
	})

	Describe("FilterNonBinaryAssetNames", func() {
		It("should filter out signature files", func() {
			assets := []string{"tool.tar.gz", "tool.tar.gz.sig", "tool.tar.gz.asc"}
			filtered := FilterNonBinaryAssetNames(assets, "linux")
			Expect(filtered).To(Equal([]string{"tool.tar.gz"}))
		})

		It("should filter out checksum files", func() {
			assets := []string{"tool.zip", "tool.zip.sha256", "checksums.txt", "tool.zip.md5"}
			filtered := FilterNonBinaryAssetNames(assets, "linux")
			Expect(filtered).To(Equal([]string{"tool.zip"}))
		})

		It("should filter out json/yaml/txt files", func() {
			assets := []string{"tool.tar.gz", "metadata.json", "config.yaml", "notes.txt"}
			filtered := FilterNonBinaryAssetNames(assets, "linux")
			Expect(filtered).To(Equal([]string{"tool.tar.gz"}))
		})

		It("should filter out .msi for non-Windows", func() {
			assets := []string{"tool.tar.gz", "tool.msi", "tool.zip"}
			filtered := FilterNonBinaryAssetNames(assets, "linux")
			Expect(filtered).To(Equal([]string{"tool.tar.gz", "tool.zip"}))

			filtered = FilterNonBinaryAssetNames(assets, "darwin")
			Expect(filtered).To(Equal([]string{"tool.tar.gz", "tool.zip"}))
		})

		It("should keep .msi for Windows", func() {
			assets := []string{"tool.tar.gz", "tool.msi", "tool.zip"}
			filtered := FilterNonBinaryAssetNames(assets, "windows")
			Expect(filtered).To(Equal([]string{"tool.tar.gz", "tool.msi", "tool.zip"}))
		})

		It("should be case insensitive", func() {
			assets := []string{"tool.ZIP", "tool.TXT", "tool.JSON"}
			filtered := FilterNonBinaryAssetNames(assets, "linux")
			Expect(filtered).To(Equal([]string{"tool.ZIP"}))
		})
	})

	Describe("NormalizeURLTemplate", func() {
		Context("URL ending with /", func() {
			It("should append {{.asset}} when URL ends with /", func() {
				url := "https://example.com/files/"
				result := NormalizeURLTemplate(url)
				Expect(result).To(Equal("https://example.com/files/{{.asset}}"))
			})

			It("should append {{.asset}} to path with version", func() {
				url := "https://archive.apache.org/dist/maven/maven-3/1.0.0/binaries/"
				result := NormalizeURLTemplate(url)
				Expect(result).To(Equal("https://archive.apache.org/dist/maven/maven-3/1.0.0/binaries/{{.asset}}"))
			})
		})

		Context("URL with {{.asset}} already present", func() {
			It("should not modify URL when {{.asset}} exists", func() {
				url := "https://example.com/files/{{.asset}}"
				result := NormalizeURLTemplate(url)
				Expect(result).To(Equal(url))
			})

			It("should not append when {{.asset}} in middle and ends with /", func() {
				url := "https://example.com/{{.asset}}/files/"
				result := NormalizeURLTemplate(url)
				Expect(result).To(Equal(url))
			})
		})

		Context("URL not ending with /", func() {
			It("should not modify URL without trailing slash", func() {
				url := "https://example.com/files"
				result := NormalizeURLTemplate(url)
				Expect(result).To(Equal(url))
			})

			It("should not modify URL with explicit filename", func() {
				url := "https://example.com/file-{{.version}}.tar.gz"
				result := NormalizeURLTemplate(url)
				Expect(result).To(Equal(url))
			})
		})

		Context("edge cases", func() {
			It("should return empty string for empty input", func() {
				result := NormalizeURLTemplate("")
				Expect(result).To(Equal(""))
			})

			It("should handle root path", func() {
				url := "https://example.com/"
				result := NormalizeURLTemplate(url)
				Expect(result).To(Equal("https://example.com/{{.asset}}"))
			})
		})
	})
})
