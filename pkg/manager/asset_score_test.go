package manager

import (
	"github.com/flanksource/deps/pkg/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// depsRelease mirrors the assets published by flanksource/deps v1.0.39, the release
// that exposed the checksum-file-installed-as-binary bug.
var depsRelease = []string{
	"deps-darwin-amd64.sha256",
	"deps-darwin-amd64.tar.gz",
	"deps-darwin-amd64.tar.gz.sha256",
	"deps-darwin-arm64.sha256",
	"deps-darwin-arm64.tar.gz",
	"deps-darwin-arm64.tar.gz.sha256",
	"deps-linux-amd64.sha256",
	"deps-linux-amd64.tar.gz",
	"deps-linux-amd64.tar.gz.sha256",
	"deps-linux-arm64.sha256",
	"deps-linux-arm64.tar.gz",
	"deps-linux-arm64.tar.gz.sha256",
	"deps-start-darwin-amd64.sha256",
	"deps-start-darwin-amd64.tar.gz",
	"deps-start-darwin-amd64.tar.gz.sha256",
	"deps-start-darwin-arm64.sha256",
	"deps-start-darwin-arm64.tar.gz",
	"deps-start-darwin-arm64.tar.gz.sha256",
	"deps-start-linux-amd64.sha256",
	"deps-start-linux-amd64.tar.gz",
	"deps-start-linux-amd64.tar.gz.sha256",
	"deps-start-linux-arm64.sha256",
	"deps-start-linux-arm64.tar.gz",
	"deps-start-linux-arm64.tar.gz.sha256",
	"deps-windows-amd64.exe",
	"deps-windows-amd64.exe.sha256",
	"deps-windows-amd64.sha256",
	"deps-windows-amd64.zip",
}

func namesToAssets(names []string) []AssetInfo {
	assets := make([]AssetInfo, len(names))
	for i, name := range names {
		assets[i] = AssetInfo{Name: name, DownloadURL: "https://example.test/" + name}
	}
	return assets
}

var _ = Describe("SelectBestAsset", func() {
	type testCase struct {
		name        string
		assets      []string
		selection   AssetSelection
		expected    string
		expectEmpty bool
	}

	tests := []testCase{
		{
			name:      "picks the real binary over the orphan checksum of the same name",
			assets:    depsRelease,
			selection: AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "deps"},
			expected:  "deps-linux-amd64.tar.gz",
		},
		{
			name:      "does not confuse a sibling package sharing the name prefix",
			assets:    depsRelease,
			selection: AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "arm64"}, PackageName: "deps"},
			expected:  "deps-linux-arm64.tar.gz",
		},
		{
			name:      "selects the sibling package when it is the requested one",
			assets:    depsRelease,
			selection: AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "deps-start"},
			expected:  "deps-start-linux-amd64.tar.gz",
		},
		{
			name:      "prefers the compressed archive over the bare executable",
			assets:    depsRelease,
			selection: AssetSelection{Platform: platform.Platform{OS: "windows", Arch: "amd64"}, PackageName: "deps"},
			expected:  "deps-windows-amd64.zip",
		},
		{
			name:      "keeps an uncompressed binary when no archive is published",
			assets:    []string{"jq-linux-amd64", "jq-linux-amd64.sha256", "jq-macos-arm64"},
			selection: AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "jq"},
			expected:  "jq-linux-amd64",
		},
		{
			name:      "accepts a version segment in the canonical name",
			assets:    []string{"tool-1.4.2-linux-amd64.tar.gz", "tool-extras-1.4.2-linux-amd64.tar.gz"},
			selection: AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "tool"},
			expected:  "tool-1.4.2-linux-amd64.tar.gz",
		},
		{
			name:      "matches underscore separated names",
			assets:    []string{"tool_linux_amd64.tar.gz", "tool_sdk_linux_amd64.tar.gz"},
			selection: AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "tool"},
			expected:  "tool_linux_amd64.tar.gz",
		},
		{
			name:      "prefers exact platform tokens over aliases",
			assets:    []string{"tool-linux-x86_64.tar.gz", "tool-linux-amd64.tar.gz"},
			selection: AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "tool"},
			expected:  "tool-linux-amd64.tar.gz",
		},
		{
			name:      "breaks ties on the shortest name",
			assets:    []string{"tool-linux-amd64-static-musl.tar.gz", "tool-linux-amd64-musl.tar.gz"},
			selection: AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "tool"},
			expected:  "tool-linux-amd64-musl.tar.gz",
		},
		{
			name:        "returns nothing when only non-binary files are offered",
			assets:      []string{"tool-linux-amd64.sha256", "checksums.txt", "README.md"},
			selection:   AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "tool"},
			expectEmpty: true,
		},
		{
			name:      "honours an explicit request for a non-binary asset",
			assets:    []string{"tool-linux-amd64.sha256"},
			selection: AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "tool", IncludeNonBinary: true},
			expected:  "tool-linux-amd64.sha256",
		},
	}

	for _, tt := range tests {
		It(tt.name, func() {
			best := SelectBestAsset(namesToAssets(tt.assets), tt.selection)
			if tt.expectEmpty {
				Expect(best).To(BeNil())
				return
			}
			Expect(best).ToNot(BeNil())
			Expect(best.Name).To(Equal(tt.expected))
		})
	}

	It("is deterministic regardless of the order assets are published in", func() {
		sel := AssetSelection{Platform: platform.Platform{OS: "linux", Arch: "amd64"}, PackageName: "deps"}
		forward := SelectBestAsset(namesToAssets(depsRelease), sel)

		reversed := make([]string, 0, len(depsRelease))
		for i := len(depsRelease) - 1; i >= 0; i-- {
			reversed = append(reversed, depsRelease[i])
		}
		backward := SelectBestAsset(namesToAssets(reversed), sel)

		Expect(forward.Name).To(Equal(backward.Name))
	})
})

var _ = Describe("PatternTargetsNonBinary", func() {
	type testCase struct {
		pattern  string
		expected bool
	}

	tests := []testCase{
		{pattern: "deps-linux-amd64", expected: false},
		{pattern: "*linux*amd64*", expected: false},
		{pattern: "deps-linux-amd64.tar.gz", expected: false},
		{pattern: "deps-linux-amd64.sha256", expected: true},
		{pattern: "*.sha256", expected: true},
		{pattern: "checksums.txt", expected: true},
		{pattern: "tool-linux-amd64.asc", expected: true},
	}

	for _, tt := range tests {
		It("reports "+tt.pattern, func() {
			Expect(PatternTargetsNonBinary(tt.pattern)).To(Equal(tt.expected))
		})
	}
})

var _ = Describe("IsCompressedAsset", func() {
	type testCase struct {
		name     string
		expected bool
	}

	tests := []testCase{
		{name: "tool-linux-amd64.tar.gz", expected: true},
		{name: "tool-linux-amd64.tgz", expected: true},
		{name: "tool-windows-amd64.zip", expected: true},
		{name: "tool-linux-amd64.tar.xz", expected: true},
		{name: "tool-linux-amd64", expected: false},
		{name: "tool-windows-amd64.exe", expected: false},
	}

	for _, tt := range tests {
		It("reports "+tt.name, func() {
			Expect(IsCompressedAsset(tt.name)).To(Equal(tt.expected))
		})
	}
})
