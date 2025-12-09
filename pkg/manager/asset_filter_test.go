package manager

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Asset Filtering", func() {
	Describe("filterNonBinaryFiles", func() {
		type testCase struct {
			name     string
			assets   []string
			expected []string
		}

		tests := []testCase{
			{
				name: "should filter out signature files",
				assets: []string{
					"tool-v1.0.0-linux-amd64",
					"tool-v1.0.0-linux-amd64.asc",
					"tool-v1.0.0-linux-amd64.sig",
					"tool-v1.0.0-linux-amd64.gpg",
				},
				expected: []string{"tool-v1.0.0-linux-amd64"},
			},
			{
				name: "should filter out checksum files",
				assets: []string{
					"tool-linux-amd64",
					"tool-linux-amd64.sha1",
					"tool-linux-amd64.sha256",
					"tool-linux-amd64.sha512",
					"tool-linux-amd64.md5",
					"checksums.txt",
				},
				expected: []string{"tool-linux-amd64"},
			},
			{
				name: "should filter out documentation files",
				assets: []string{
					"binary-darwin-arm64.tar.gz",
					"README.md",
					"CHANGELOG.txt",
					"LICENSE",
					"COPYING",
					"AUTHORS",
				},
				expected: []string{"binary-darwin-arm64.tar.gz"},
			},
			{
				name: "should keep all binary files",
				assets: []string{
					"tool-linux-amd64",
					"tool-darwin-arm64.tar.gz",
					"tool-windows-x64.zip",
				},
				expected: []string{
					"tool-linux-amd64",
					"tool-darwin-arm64.tar.gz",
					"tool-windows-x64.zip",
				},
			},
		}

		for _, tt := range tests {
			It(tt.name, func() {
				var assets []AssetInfo
				for _, name := range tt.assets {
					assets = append(assets, AssetInfo{Name: name})
				}

				filtered := filterNonBinaryFiles(assets)
				Expect(filtered).To(HaveLen(len(tt.expected)))
				for i, exp := range tt.expected {
					Expect(filtered[i].Name).To(Equal(exp))
				}
			})
		}
	})

	Describe("getOSAliases", func() {
		type testCase struct {
			name     string
			os       string
			expected []string
		}

		tests := []testCase{
			{
				name:     "should return darwin aliases",
				os:       "darwin",
				expected: []string{"darwin", "mac", "macos", "osx"},
			},
			{
				name:     "should return windows aliases",
				os:       "windows",
				expected: []string{"windows", "win", "win32", "win64"},
			},
			{
				name:     "should return linux aliases",
				os:       "linux",
				expected: []string{"linux"},
			},
			{
				name:     "should return unknown OS as-is",
				os:       "freebsd",
				expected: []string{"freebsd"},
			},
		}

		for _, tt := range tests {
			It(tt.name, func() {
				aliases := getOSAliases(tt.os)
				Expect(aliases).To(ConsistOf(tt.expected))
			})
		}
	})

	Describe("filterByOS", func() {
		type testCase struct {
			name     string
			assets   []string
			os       string
			expected []string
			wantErr  bool
		}

		tests := []testCase{
			{
				name: "should filter by darwin using 'darwin' in filename",
				assets: []string{
					"tool-darwin-arm64",
					"tool-linux-amd64",
					"tool-windows-x64.exe",
				},
				os:       "darwin",
				expected: []string{"tool-darwin-arm64"},
			},
			{
				name: "should filter by darwin using 'mac' alias",
				assets: []string{
					"tool-mac-arm64",
					"tool-linux-amd64",
				},
				os:       "darwin",
				expected: []string{"tool-mac-arm64"},
			},
			{
				name: "should filter by darwin using 'macos' alias",
				assets: []string{
					"tool-macos-amd64.tar.gz",
					"tool-linux-amd64.tar.gz",
				},
				os:       "darwin",
				expected: []string{"tool-macos-amd64.tar.gz"},
			},
			{
				name: "should filter by windows using 'win' alias",
				assets: []string{
					"tool-win-x64.exe",
					"tool-linux-amd64",
				},
				os:       "windows",
				expected: []string{"tool-win-x64.exe"},
			},
			{
				name: "should be case-insensitive",
				assets: []string{
					"Tool-Darwin-ARM64.tar.gz",
					"Tool-Linux-AMD64.tar.gz",
				},
				os:       "darwin",
				expected: []string{"Tool-Darwin-ARM64.tar.gz"},
			},
			{
				name: "should return all assets if no OS-specific files found (universal binary)",
				assets: []string{
					"universal-binary.tar.gz",
					"another-binary",
				},
				os:       "darwin",
				expected: []string{"universal-binary.tar.gz", "another-binary"},
			},
		}

		for _, tt := range tests {
			It(tt.name, func() {
				var assets []AssetInfo
				for _, name := range tt.assets {
					assets = append(assets, AssetInfo{Name: name})
				}

				filtered, err := filterByOS(assets, tt.os)
				if tt.wantErr {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(filtered).To(HaveLen(len(tt.expected)))
					for i, exp := range tt.expected {
						Expect(filtered[i].Name).To(Equal(exp))
					}
				}
			})
		}
	})

	Describe("filterByArch", func() {
		type testCase struct {
			name     string
			assets   []string
			arch     string
			expected []string
			wantErr  bool
		}

		tests := []testCase{
			{
				name: "should filter by amd64",
				assets: []string{
					"tool-linux-amd64",
					"tool-linux-arm64",
				},
				arch:     "amd64",
				expected: []string{"tool-linux-amd64"},
			},
			{
				name: "should filter by amd64 using x86_64 alias",
				assets: []string{
					"tool-linux-x86_64.tar.gz",
					"tool-linux-arm64.tar.gz",
				},
				arch:     "amd64",
				expected: []string{"tool-linux-x86_64.tar.gz"},
			},
			{
				name: "should filter by amd64 using x64 alias",
				assets: []string{
					"tool-win-x64.exe",
					"tool-win-arm64.exe",
				},
				arch:     "amd64",
				expected: []string{"tool-win-x64.exe"},
			},
			{
				name: "should filter by amd64 using i386 alias",
				assets: []string{
					"tool-linux-i386",
					"tool-linux-arm",
				},
				arch:     "amd64",
				expected: []string{"tool-linux-i386"},
			},
			{
				name: "should filter by amd64 using x86-64 alias",
				assets: []string{
					"tool-linux-x86-64.tar.gz",
					"tool-linux-arm64.tar.gz",
				},
				arch:     "amd64",
				expected: []string{"tool-linux-x86-64.tar.gz"},
			},
			{
				name: "should filter by amd64 using 64bit alias",
				assets: []string{
					"binary-windows-64bit.exe",
					"binary-windows-arm64.exe",
				},
				arch:     "amd64",
				expected: []string{"binary-windows-64bit.exe"},
			},
			{
				name: "should filter by amd64 using 64-bit alias",
				assets: []string{
					"tool-darwin-64-bit",
					"tool-darwin-arm64",
				},
				arch:     "amd64",
				expected: []string{"tool-darwin-64-bit"},
			},
			{
				name: "should filter by arm64 using aarch64 alias",
				assets: []string{
					"tool-linux-aarch64.tar.gz",
					"tool-linux-x86_64.tar.gz",
				},
				arch:     "arm64",
				expected: []string{"tool-linux-aarch64.tar.gz"},
			},
			{
				name: "should be case-insensitive",
				assets: []string{
					"Tool-Linux-AMD64.tar.gz",
					"Tool-Linux-ARM64.tar.gz",
				},
				arch:     "amd64",
				expected: []string{"Tool-Linux-AMD64.tar.gz"},
			},
			{
				name: "should return all assets if no arch-specific files found (universal binary)",
				assets: []string{
					"universal-binary",
				},
				arch:     "amd64",
				expected: []string{"universal-binary"},
			},
			{
				name: "should prefer arm64 over arm when both exist",
				assets: []string{
					"yq_linux_arm.tar.gz",
					"yq_linux_arm64.tar.gz",
				},
				arch:     "arm64",
				expected: []string{"yq_linux_arm64.tar.gz"},
			},
			{
				name: "should fallback to arm when only arm exists and arm64 requested",
				assets: []string{
					"tool-linux-arm.tar.gz",
				},
				arch:     "arm64",
				expected: []string{"tool-linux-arm.tar.gz"},
			},
			{
				name: "should prefer aarch64 over arm when both exist",
				assets: []string{
					"tool-linux-aarch64.tar.gz",
					"tool-linux-arm.tar.gz",
				},
				arch:     "arm64",
				expected: []string{"tool-linux-aarch64.tar.gz"},
			},
		}

		for _, tt := range tests {
			It(tt.name, func() {
				var assets []AssetInfo
				for _, name := range tt.assets {
					assets = append(assets, AssetInfo{Name: name})
				}

				filtered, err := filterByArch(assets, tt.arch)
				if tt.wantErr {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(filtered).To(HaveLen(len(tt.expected)))
					for i, exp := range tt.expected {
						Expect(filtered[i].Name).To(Equal(exp))
					}
				}
			})
		}
	})

	Describe("FilterAssetsByPlatform", func() {
		type testCase struct {
			name     string
			assets   []string
			os       string
			arch     string
			expected []string
			wantErr  bool
		}

		tests := []testCase{
			{
				name: "should apply all three filtering stages",
				assets: []string{
					"tool-v1.0.0-linux-amd64",
					"tool-v1.0.0-linux-amd64.sha256",
					"tool-v1.0.0-linux-arm64",
					"tool-v1.0.0-darwin-amd64",
					"tool-v1.0.0-darwin-arm64",
					"tool-v1.0.0-windows-x64.exe",
					"README.md",
				},
				os:       "darwin",
				arch:     "arm64",
				expected: []string{"tool-v1.0.0-darwin-arm64"},
			},
			{
				name: "should work with OS and arch aliases",
				assets: []string{
					"kubectl-macos-x86_64.tar.gz",
					"kubectl-linux-amd64.tar.gz",
					"kubectl-win-x64.zip",
				},
				os:       "darwin",
				arch:     "amd64",
				expected: []string{"kubectl-macos-x86_64.tar.gz"},
			},
			{
				name: "should filter checksums and signatures first",
				assets: []string{
					"helm-darwin-arm64.tar.gz",
					"helm-darwin-arm64.tar.gz.sha256",
					"helm-darwin-arm64.tar.gz.asc",
					"helm-linux-amd64.tar.gz",
				},
				os:       "darwin",
				arch:     "arm64",
				expected: []string{"helm-darwin-arm64.tar.gz"},
			},
			{
				name: "should handle i386 mapping to x64",
				assets: []string{
					"legacy-tool-linux-i386",
					"legacy-tool-linux-arm",
				},
				os:       "linux",
				arch:     "amd64",
				expected: []string{"legacy-tool-linux-i386"},
			},
			{
				name: "should handle i686 mapping to x64",
				assets: []string{
					"tool-linux-i686.tar.gz",
					"tool-linux-armv7.tar.gz",
				},
				os:       "linux",
				arch:     "amd64",
				expected: []string{"tool-linux-i686.tar.gz"},
			},
			{
				name:    "should return error when no assets provided",
				assets:  []string{},
				os:      "darwin",
				arch:    "arm64",
				wantErr: true,
			},
			{
				name: "should return error when all assets are non-binary",
				assets: []string{
					"checksums.txt",
					"README.md",
					"LICENSE",
				},
				os:      "darwin",
				arch:    "arm64",
				wantErr: true,
			},
			{
				name: "should handle real-world GitHub release assets",
				assets: []string{
					"yq_darwin_amd64.tar.gz",
					"yq_darwin_amd64.tar.gz.sha256",
					"yq_darwin_arm64.tar.gz",
					"yq_linux_386.tar.gz",
					"yq_linux_amd64.tar.gz",
					"yq_linux_arm.tar.gz",
					"yq_linux_arm64.tar.gz",
					"yq_windows_386.zip",
					"yq_windows_amd64.zip",
					"checksums",
					"checksums_hashes_order",
					"extract-checksum.sh",
				},
				os:       "darwin",
				arch:     "arm64",
				expected: []string{"yq_darwin_arm64.tar.gz"},
			},
			{
				name: "should handle kubectl-style naming",
				assets: []string{
					"kubectl",
					"kubectl.exe",
					"kubectl.sha256",
				},
				os:       "linux",
				arch:     "amd64",
				expected: []string{"kubectl", "kubectl.exe"},
			},
		}

		for _, tt := range tests {
			It(tt.name, func() {
				var assets []AssetInfo
				for _, name := range tt.assets {
					assets = append(assets, AssetInfo{Name: name})
				}

				filtered, err := FilterAssetsByPlatform(assets, tt.os, tt.arch)
				if tt.wantErr {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(filtered).To(HaveLen(len(tt.expected)))
					for i, exp := range tt.expected {
						Expect(filtered[i].Name).To(Equal(exp))
					}
				}
			})
		}
	})
})
