package manager

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Asset Filtering", func() {
	Describe("filterNonBinaryFiles", func() {
		It("should filter out signature files", func() {
			assets := []AssetInfo{
				{Name: "tool-v1.0.0-linux-amd64"},
				{Name: "tool-v1.0.0-linux-amd64.asc"},
				{Name: "tool-v1.0.0-linux-amd64.sig"},
				{Name: "tool-v1.0.0-linux-amd64.gpg"},
			}

			filtered := filterNonBinaryFiles(assets)
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-v1.0.0-linux-amd64"))
		})

		It("should filter out checksum files", func() {
			assets := []AssetInfo{
				{Name: "tool-linux-amd64"},
				{Name: "tool-linux-amd64.sha1"},
				{Name: "tool-linux-amd64.sha256"},
				{Name: "tool-linux-amd64.sha512"},
				{Name: "tool-linux-amd64.md5"},
				{Name: "checksums.txt"},
			}

			filtered := filterNonBinaryFiles(assets)
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-linux-amd64"))
		})

		It("should filter out documentation files", func() {
			assets := []AssetInfo{
				{Name: "binary-darwin-arm64.tar.gz"},
				{Name: "README.md"},
				{Name: "CHANGELOG.txt"},
				{Name: "LICENSE"},
				{Name: "COPYING"},
				{Name: "AUTHORS"},
			}

			filtered := filterNonBinaryFiles(assets)
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("binary-darwin-arm64.tar.gz"))
		})

		It("should keep all binary files", func() {
			assets := []AssetInfo{
				{Name: "tool-linux-amd64"},
				{Name: "tool-darwin-arm64.tar.gz"},
				{Name: "tool-windows-x64.zip"},
			}

			filtered := filterNonBinaryFiles(assets)
			Expect(filtered).To(HaveLen(3))
		})
	})

	Describe("getOSAliases", func() {
		It("should return darwin aliases", func() {
			aliases := getOSAliases("darwin")
			Expect(aliases).To(ConsistOf("darwin", "mac", "macos", "osx"))
		})

		It("should return windows aliases", func() {
			aliases := getOSAliases("windows")
			Expect(aliases).To(ConsistOf("windows", "win", "win32", "win64"))
		})

		It("should return linux aliases", func() {
			aliases := getOSAliases("linux")
			Expect(aliases).To(ConsistOf("linux"))
		})

		It("should return unknown OS as-is", func() {
			aliases := getOSAliases("freebsd")
			Expect(aliases).To(ConsistOf("freebsd"))
		})
	})

	Describe("filterByOS", func() {
		It("should filter by darwin using 'darwin' in filename", func() {
			assets := []AssetInfo{
				{Name: "tool-darwin-arm64"},
				{Name: "tool-linux-amd64"},
				{Name: "tool-windows-x64.exe"},
			}

			filtered, err := filterByOS(assets, "darwin")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-darwin-arm64"))
		})

		It("should filter by darwin using 'mac' alias", func() {
			assets := []AssetInfo{
				{Name: "tool-mac-arm64"},
				{Name: "tool-linux-amd64"},
			}

			filtered, err := filterByOS(assets, "darwin")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-mac-arm64"))
		})

		It("should filter by darwin using 'macos' alias", func() {
			assets := []AssetInfo{
				{Name: "tool-macos-amd64.tar.gz"},
				{Name: "tool-linux-amd64.tar.gz"},
			}

			filtered, err := filterByOS(assets, "darwin")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-macos-amd64.tar.gz"))
		})

		It("should filter by windows using 'win' alias", func() {
			assets := []AssetInfo{
				{Name: "tool-win-x64.exe"},
				{Name: "tool-linux-amd64"},
			}

			filtered, err := filterByOS(assets, "windows")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-win-x64.exe"))
		})

		It("should be case-insensitive", func() {
			assets := []AssetInfo{
				{Name: "Tool-Darwin-ARM64.tar.gz"},
				{Name: "Tool-Linux-AMD64.tar.gz"},
			}

			filtered, err := filterByOS(assets, "darwin")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("Tool-Darwin-ARM64.tar.gz"))
		})

		It("should return all assets if no OS-specific files found (universal binary)", func() {
			assets := []AssetInfo{
				{Name: "universal-binary.tar.gz"},
				{Name: "another-binary"},
			}

			filtered, err := filterByOS(assets, "darwin")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(2))
		})
	})

	Describe("getArchAliases", func() {
		It("should return amd64 aliases including x86 variants", func() {
			aliases := getArchAliases("amd64")
			Expect(aliases).To(ConsistOf("amd64", "x86_64", "x64", "x86-64", "i386", "i686", "x86", "386", "64bit", "64-bit"))
		})

		It("should return amd64 aliases for x86_64", func() {
			aliases := getArchAliases("x86_64")
			Expect(aliases).To(ConsistOf("amd64", "x86_64", "x64", "x86-64", "i386", "i686", "x86", "386", "64bit", "64-bit"))
		})

		It("should return amd64 aliases for i386", func() {
			aliases := getArchAliases("i386")
			Expect(aliases).To(ConsistOf("amd64", "x86_64", "x64", "x86-64", "i386", "i686", "x86", "386", "64bit", "64-bit"))
		})

		It("should return amd64 aliases for x86-64", func() {
			aliases := getArchAliases("x86-64")
			Expect(aliases).To(ConsistOf("amd64", "x86_64", "x64", "x86-64", "i386", "i686", "x86", "386", "64bit", "64-bit"))
		})

		It("should return amd64 aliases for 64bit", func() {
			aliases := getArchAliases("64bit")
			Expect(aliases).To(ConsistOf("amd64", "x86_64", "x64", "x86-64", "i386", "i686", "x86", "386", "64bit", "64-bit"))
		})

		It("should return amd64 aliases for 64-bit", func() {
			aliases := getArchAliases("64-bit")
			Expect(aliases).To(ConsistOf("amd64", "x86_64", "x64", "x86-64", "i386", "i686", "x86", "386", "64bit", "64-bit"))
		})

		It("should return arm64 aliases", func() {
			aliases := getArchAliases("arm64")
			Expect(aliases).To(ConsistOf("arm64", "aarch64", "arm"))
		})

		It("should return arm aliases", func() {
			aliases := getArchAliases("arm")
			Expect(aliases).To(ConsistOf("arm", "armv7", "armv7l"))
		})

		It("should return unknown arch as-is", func() {
			aliases := getArchAliases("riscv64")
			Expect(aliases).To(ConsistOf("riscv64"))
		})
	})

	Describe("filterByArch", func() {
		It("should filter by amd64", func() {
			assets := []AssetInfo{
				{Name: "tool-linux-amd64"},
				{Name: "tool-linux-arm64"},
			}

			filtered, err := filterByArch(assets, "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-linux-amd64"))
		})

		It("should filter by amd64 using x86_64 alias", func() {
			assets := []AssetInfo{
				{Name: "tool-linux-x86_64.tar.gz"},
				{Name: "tool-linux-arm64.tar.gz"},
			}

			filtered, err := filterByArch(assets, "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-linux-x86_64.tar.gz"))
		})

		It("should filter by amd64 using x64 alias", func() {
			assets := []AssetInfo{
				{Name: "tool-win-x64.exe"},
				{Name: "tool-win-arm64.exe"},
			}

			filtered, err := filterByArch(assets, "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-win-x64.exe"))
		})

		It("should filter by amd64 using i386 alias", func() {
			assets := []AssetInfo{
				{Name: "tool-linux-i386"},
				{Name: "tool-linux-arm"},
			}

			filtered, err := filterByArch(assets, "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-linux-i386"))
		})

		It("should filter by amd64 using x86-64 alias", func() {
			assets := []AssetInfo{
				{Name: "tool-linux-x86-64.tar.gz"},
				{Name: "tool-linux-arm64.tar.gz"},
			}

			filtered, err := filterByArch(assets, "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-linux-x86-64.tar.gz"))
		})

		It("should filter by amd64 using 64bit alias", func() {
			assets := []AssetInfo{
				{Name: "binary-windows-64bit.exe"},
				{Name: "binary-windows-arm64.exe"},
			}

			filtered, err := filterByArch(assets, "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("binary-windows-64bit.exe"))
		})

		It("should filter by amd64 using 64-bit alias", func() {
			assets := []AssetInfo{
				{Name: "tool-darwin-64-bit"},
				{Name: "tool-darwin-arm64"},
			}

			filtered, err := filterByArch(assets, "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-darwin-64-bit"))
		})

		It("should filter by arm64 using aarch64 alias", func() {
			assets := []AssetInfo{
				{Name: "tool-linux-aarch64.tar.gz"},
				{Name: "tool-linux-x86_64.tar.gz"},
			}

			filtered, err := filterByArch(assets, "arm64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-linux-aarch64.tar.gz"))
		})

		It("should be case-insensitive", func() {
			assets := []AssetInfo{
				{Name: "Tool-Linux-AMD64.tar.gz"},
				{Name: "Tool-Linux-ARM64.tar.gz"},
			}

			filtered, err := filterByArch(assets, "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("Tool-Linux-AMD64.tar.gz"))
		})

		It("should return all assets if no arch-specific files found (universal binary)", func() {
			assets := []AssetInfo{
				{Name: "universal-binary"},
			}

			filtered, err := filterByArch(assets, "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
		})
	})

	Describe("FilterAssetsByPlatform", func() {
		It("should apply all three filtering stages", func() {
			assets := []AssetInfo{
				{Name: "tool-v1.0.0-linux-amd64"},
				{Name: "tool-v1.0.0-linux-amd64.sha256"},
				{Name: "tool-v1.0.0-linux-arm64"},
				{Name: "tool-v1.0.0-darwin-amd64"},
				{Name: "tool-v1.0.0-darwin-arm64"},
				{Name: "tool-v1.0.0-windows-x64.exe"},
				{Name: "README.md"},
			}

			filtered, err := FilterAssetsByPlatform(assets, "darwin", "arm64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-v1.0.0-darwin-arm64"))
		})

		It("should work with OS and arch aliases", func() {
			assets := []AssetInfo{
				{Name: "kubectl-macos-x86_64.tar.gz"},
				{Name: "kubectl-linux-amd64.tar.gz"},
				{Name: "kubectl-win-x64.zip"},
			}

			filtered, err := FilterAssetsByPlatform(assets, "darwin", "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("kubectl-macos-x86_64.tar.gz"))
		})

		It("should filter checksums and signatures first", func() {
			assets := []AssetInfo{
				{Name: "helm-darwin-arm64.tar.gz"},
				{Name: "helm-darwin-arm64.tar.gz.sha256"},
				{Name: "helm-darwin-arm64.tar.gz.asc"},
				{Name: "helm-linux-amd64.tar.gz"},
			}

			filtered, err := FilterAssetsByPlatform(assets, "darwin", "arm64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("helm-darwin-arm64.tar.gz"))
		})

		It("should handle i386 mapping to x64", func() {
			assets := []AssetInfo{
				{Name: "legacy-tool-linux-i386"},
				{Name: "legacy-tool-linux-arm"},
			}

			filtered, err := FilterAssetsByPlatform(assets, "linux", "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("legacy-tool-linux-i386"))
		})

		It("should handle i686 mapping to x64", func() {
			assets := []AssetInfo{
				{Name: "tool-linux-i686.tar.gz"},
				{Name: "tool-linux-armv7.tar.gz"},
			}

			filtered, err := FilterAssetsByPlatform(assets, "linux", "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("tool-linux-i686.tar.gz"))
		})

		It("should return error when no assets provided", func() {
			_, err := FilterAssetsByPlatform([]AssetInfo{}, "darwin", "arm64")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no assets provided"))
		})

		It("should return error when all assets are non-binary", func() {
			assets := []AssetInfo{
				{Name: "checksums.txt"},
				{Name: "README.md"},
				{Name: "LICENSE"},
			}

			_, err := FilterAssetsByPlatform(assets, "darwin", "arm64")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no assets found after filtering non-binary files"))
		})

		It("should handle real-world GitHub release assets", func() {
			assets := []AssetInfo{
				{Name: "yq_darwin_amd64.tar.gz"},
				{Name: "yq_darwin_amd64.tar.gz.sha256"},
				{Name: "yq_darwin_arm64.tar.gz"},
				{Name: "yq_linux_386.tar.gz"},
				{Name: "yq_linux_amd64.tar.gz"},
				{Name: "yq_linux_arm.tar.gz"},
				{Name: "yq_linux_arm64.tar.gz"},
				{Name: "yq_windows_386.zip"},
				{Name: "yq_windows_amd64.zip"},
				{Name: "checksums"},
				{Name: "checksums_hashes_order"},
				{Name: "extract-checksum.sh"},
			}

			filtered, err := FilterAssetsByPlatform(assets, "darwin", "arm64")
			Expect(err).NotTo(HaveOccurred())
			Expect(filtered).To(HaveLen(1))
			Expect(filtered[0].Name).To(Equal("yq_darwin_arm64.tar.gz"))
		})

		It("should handle kubectl-style naming", func() {
			assets := []AssetInfo{
				{Name: "kubectl"},
				{Name: "kubectl.exe"},
				{Name: "kubectl.sha256"},
			}

			// Without OS/arch in name, should return all binary files
			filtered, err := FilterAssetsByPlatform(assets, "linux", "amd64")
			Expect(err).NotTo(HaveOccurred())
			Expect(len(filtered)).To(BeNumerically(">", 0))
		})
	})
})
