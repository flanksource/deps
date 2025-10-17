package github

import (
	"context"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GitHubBuildManager", func() {
	var manager *GitHubBuildManager
	var ctx context.Context

	BeforeEach(func() {
		manager = NewGitHubBuildManager()
		ctx = context.Background()
	})

	Describe("Name", func() {
		It("should return the correct manager name", func() {
			Expect(manager.Name()).To(Equal("github_build"))
		})
	})

	Describe("parseVersion", func() {
		It("should parse version without build date", func() {
			buildTag, softwareVersion := parseVersion("3.11")
			Expect(buildTag).To(Equal("latest"))
			Expect(softwareVersion).To(Equal("3.11"))
		})

		It("should parse version with build date prefix", func() {
			buildTag, softwareVersion := parseVersion("20251010-3.11.14")
			Expect(buildTag).To(Equal("20251010"))
			Expect(softwareVersion).To(Equal("3.11.14"))
		})

		It("should parse full version number", func() {
			buildTag, softwareVersion := parseVersion("3.11.14")
			Expect(buildTag).To(Equal("latest"))
			Expect(softwareVersion).To(Equal("3.11.14"))
		})

		It("should handle version with build date and minor version", func() {
			buildTag, softwareVersion := parseVersion("20251010-3.11")
			Expect(buildTag).To(Equal("20251010"))
			Expect(softwareVersion).To(Equal("3.11"))
		})

		It("should parse build date only as latest software version", func() {
			buildTag, softwareVersion := parseVersion("20251010")
			Expect(buildTag).To(Equal("20251010"))
			Expect(softwareVersion).To(Equal("latest"))
		})

		It("should parse latest keyword", func() {
			buildTag, softwareVersion := parseVersion("latest")
			Expect(buildTag).To(Equal("latest"))
			Expect(softwareVersion).To(Equal("latest"))
		})
	})

	Describe("isNumeric", func() {
		It("should return true for numeric strings", func() {
			Expect(isNumeric("12345678")).To(BeTrue())
			Expect(isNumeric("0")).To(BeTrue())
		})

		It("should return false for non-numeric strings", func() {
			Expect(isNumeric("abc")).To(BeFalse())
			Expect(isNumeric("123abc")).To(BeFalse())
			Expect(isNumeric("")).To(BeFalse())
		})
	})

	Describe("parseAssetName", func() {
		It("should parse cpython asset name correctly", func() {
			assetName := "cpython-3.11.14+20251010-aarch64-apple-darwin-install_only.tar.gz"
			version, buildDate, platformStr, err := parseAssetName(assetName)
			Expect(err).ToNot(HaveOccurred())
			Expect(version).To(Equal("3.11.14"))
			Expect(buildDate).To(Equal("20251010"))
			Expect(platformStr).To(Equal("aarch64-apple-darwin"))
		})

		It("should parse different platform asset", func() {
			assetName := "cpython-3.12.0+20240101-x86_64-unknown-linux-gnu-install_only.tar.gz"
			version, buildDate, platformStr, err := parseAssetName(assetName)
			Expect(err).ToNot(HaveOccurred())
			Expect(version).To(Equal("3.12.0"))
			Expect(buildDate).To(Equal("20240101"))
			Expect(platformStr).To(Equal("x86_64-unknown-linux-gnu"))
		})

		It("should return error for invalid asset name", func() {
			assetName := "invalid-asset-name.tar.gz"
			_, _, _, err := parseAssetName(assetName)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse asset name"))
		})
	})

	Describe("parseAssetsWithDigests", func() {
		It("should filter assets by platform", func() {
			assets := []AssetInfo{
				{
					Name:               "cpython-3.11.14+20251010-aarch64-apple-darwin-install_only.tar.gz",
					BrowserDownloadURL: "https://example.com/darwin-arm64.tar.gz",
					SHA256:             "abc123",
				},
				{
					Name:               "cpython-3.11.14+20251010-x86_64-apple-darwin-install_only.tar.gz",
					BrowserDownloadURL: "https://example.com/darwin-amd64.tar.gz",
					SHA256:             "def456",
				},
				{
					Name:               "cpython-3.11.14+20251010-x86_64-unknown-linux-gnu-install_only.tar.gz",
					BrowserDownloadURL: "https://example.com/linux-amd64.tar.gz",
					SHA256:             "ghi789",
				},
			}

			plat := platform.Platform{OS: "darwin", Arch: "arm64"}
			result, err := manager.parseAssetsWithDigests(assets, plat)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].softwareVer).To(Equal("3.11.14"))
			Expect(result[0].platformStr).To(Equal("aarch64-apple-darwin"))
		})

		It("should skip non-install_only assets", func() {
			assets := []AssetInfo{
				{
					Name:               "cpython-3.11.14+20251010-aarch64-apple-darwin-debug.tar.gz",
					BrowserDownloadURL: "https://example.com/debug.tar.gz",
					SHA256:             "abc123",
				},
				{
					Name:               "cpython-3.11.14+20251010-aarch64-apple-darwin-install_only.tar.gz",
					BrowserDownloadURL: "https://example.com/install.tar.gz",
					SHA256:             "def456",
				},
			}

			plat := platform.Platform{OS: "darwin", Arch: "arm64"}
			result, err := manager.parseAssetsWithDigests(assets, plat)

			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].assetName).To(ContainSubstring("install_only"))
		})
	})

	Describe("findMatchingVersion", func() {
		var testAssets []assetVersion

		BeforeEach(func() {
			testAssets = []assetVersion{
				{softwareVer: "3.11.14", buildDate: "20251010"},
				{softwareVer: "3.11.13", buildDate: "20251010"},
				{softwareVer: "3.12.0", buildDate: "20251010"},
			}
		})

		It("should find exact version match", func() {
			matched, err := manager.findMatchingVersion(testAssets, "3.11.14")
			Expect(err).ToNot(HaveOccurred())
			Expect(matched.softwareVer).To(Equal("3.11.14"))
		})

		It("should find constraint match for minor version", func() {
			matched, err := manager.findMatchingVersion(testAssets, "3.11")
			Expect(err).ToNot(HaveOccurred())
			// Should match highest 3.11.x version
			Expect(matched.softwareVer).To(Equal("3.11.14"))
		})

		It("should return error when no match found", func() {
			_, err := manager.findMatchingVersion(testAssets, "3.10")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("version 3.10 not found"))
		})

		It("should return error when assets array is empty", func() {
			_, err := manager.findMatchingVersion([]assetVersion{}, "3.11")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no assets available"))
		})
	})

	Describe("DiscoverVersions", func() {
		Context("with valid python-build-standalone repository", func() {
			It("should return versions from latest release", func() {
				pkg := types.Package{
					Name: "cpython",
					Repo: "astral-sh/python-build-standalone",
				}
				plat := platform.Platform{OS: "darwin", Arch: "arm64"}

				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 5)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				// Verify versions contain semantic versions
				for _, v := range versions {
					Expect(v.Version).To(MatchRegexp(`^\d+\.\d+\.\d+$`))
					Expect(v.Tag).ToNot(BeEmpty()) // Should contain build date
				}

				// Verify versions are sorted (newest first)
				if len(versions) > 1 {
					first := versions[0]
					second := versions[1]
					Expect(first.Version).ToNot(BeEmpty())
					Expect(second.Version).ToNot(BeEmpty())
				}
			})
		})

		Context("with missing repo", func() {
			It("should return error when repo is missing", func() {
				pkg := types.Package{
					Name: "cpython",
					Repo: "",
				}
				plat := platform.Platform{OS: "darwin", Arch: "arm64"}

				_, err := manager.DiscoverVersions(ctx, pkg, plat, 10)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("repo is required"))
			})
		})

		Context("with invalid repo format", func() {
			It("should return error when repo format is invalid", func() {
				pkg := types.Package{
					Name: "cpython",
					Repo: "invalid-format",
				}
				plat := platform.Platform{OS: "darwin", Arch: "arm64"}

				_, err := manager.DiscoverVersions(ctx, pkg, plat, 10)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid repo format"))
			})
		})
	})

	Describe("Resolve", func() {
		Context("with version without build date", func() {
			It("should resolve using latest release", func() {
				pkg := types.Package{
					Name: "cpython",
					Repo: "astral-sh/python-build-standalone",
				}
				plat := platform.Platform{OS: "darwin", Arch: "arm64"}

				// First discover available versions
				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 1)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				testVersion := versions[0].Version

				resolution, err := manager.Resolve(ctx, pkg, testVersion, plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(resolution).ToNot(BeNil())
				Expect(resolution.Version).To(Equal(testVersion))
				Expect(resolution.DownloadURL).To(ContainSubstring("install_only.tar.gz"))
				Expect(resolution.IsArchive).To(BeTrue())
				Expect(resolution.GitHubAsset).ToNot(BeNil())
				Expect(resolution.GitHubAsset.Repo).To(Equal("astral-sh/python-build-standalone"))
			})
		})

		Context("with constraint version", func() {
			It("should resolve to highest matching version", func() {
				pkg := types.Package{
					Name: "cpython",
					Repo: "astral-sh/python-build-standalone",
				}
				plat := platform.Platform{OS: "darwin", Arch: "arm64"}

				// Discover versions to find available versions
				versions, err := manager.DiscoverVersions(ctx, pkg, plat, 5)
				Expect(err).ToNot(HaveOccurred())
				Expect(versions).ToNot(BeEmpty())

				// Extract minor version from first result (e.g., "3.11" from "3.11.14")
				minorVersion := versions[0].Version[:4] // e.g., "3.11"

				resolution, err := manager.Resolve(ctx, pkg, minorVersion, plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(resolution).ToNot(BeNil())
				Expect(resolution.Version).To(HavePrefix(minorVersion))
			})
		})

		Context("with invalid repo format", func() {
			It("should return error", func() {
				pkg := types.Package{
					Name: "cpython",
					Repo: "invalid",
				}
				plat := platform.Platform{OS: "darwin", Arch: "arm64"}

				_, err := manager.Resolve(ctx, pkg, "3.11", plat)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid repo format"))
			})
		})
	})

	Describe("guessBinaryPath", func() {
		It("should use BinaryPath if specified", func() {
			pkg := types.Package{
				Name:       "cpython",
				BinaryPath: "python/bin/python3",
			}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			path := manager.guessBinaryPath(pkg, "cpython-3.11.14+20251010-aarch64-apple-darwin-install_only.tar.gz", plat)
			Expect(path).To(Equal("python/bin/python3"))
		})

		It("should use BinaryName if specified", func() {
			pkg := types.Package{
				Name:       "cpython",
				BinaryName: "python3",
			}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			path := manager.guessBinaryPath(pkg, "cpython-3.11.14+20251010-aarch64-apple-darwin-install_only.tar.gz", plat)
			Expect(path).To(Equal("python3"))
		})

		It("should default to package name", func() {
			pkg := types.Package{
				Name: "cpython",
			}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			path := manager.guessBinaryPath(pkg, "cpython-3.11.14+20251010-aarch64-apple-darwin-install_only.tar.gz", plat)
			Expect(path).To(Equal("cpython"))
		})

		It("should add .exe extension on Windows", func() {
			pkg := types.Package{
				Name: "cpython",
			}
			plat := platform.Platform{OS: "windows", Arch: "amd64"}

			path := manager.guessBinaryPath(pkg, "cpython-3.11.14+20251010-x86_64-pc-windows-msvc-install_only.tar.gz", plat)
			Expect(path).To(Equal("cpython.exe"))
		})
	})

	Describe("GetChecksums", func() {
		It("should return nil as checksums are embedded in asset digests", func() {
			pkg := types.Package{
				Name: "cpython",
				Repo: "astral-sh/python-build-standalone",
			}

			checksums, err := manager.GetChecksums(ctx, pkg, "3.11.14")
			Expect(err).ToNot(HaveOccurred())
			Expect(checksums).To(BeNil())
		})
	})

	Describe("WhoAmI", func() {
		It("should return authentication status", func() {
			status := manager.WhoAmI(ctx)
			Expect(status).ToNot(BeNil())
			Expect(status.Service).To(Equal("GitHub"))
		})
	})

	Describe("Verify", func() {
		It("should return not implemented error", func() {
			_, err := manager.Verify(ctx, "/path/to/binary", types.Package{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("verify not implemented"))
		})
	})

	Describe("Install", func() {
		It("should return not implemented error", func() {
			resolution := &types.Resolution{}
			err := manager.Install(ctx, resolution, types.InstallOptions{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("install method not yet implemented"))
		})
	})
})
