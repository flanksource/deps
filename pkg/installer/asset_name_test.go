package installer

import (
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("selected asset install identity", func() {
	DescribeTable("derives a binary name without version or platform metadata",
		func(assetName, version, expected string) {
			name, err := deriveAssetInstallName(assetName, version, platform.Platform{OS: "darwin", Arch: "amd64"})
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal(expected))
		},
		Entry("platform suffix", "faro_darwin_amd64", "0.0.1887", "faro"),
		Entry("version and platform suffix", "faro_0.0.1887_darwin_amd64", "0.0.1887", "faro"),
		Entry("hyphenated product", "mission-control-v0.0.1887-darwin-amd64.tar.gz", "0.0.1887", "mission-control"),
		Entry("windows executable", "faro_windows_amd64.exe", "0.0.1887", "faro"),
		Entry("name without metadata", "faro-cli", "0.0.1887", "faro-cli"),
	)

	It("uses the derived name only for an explicit asset override", func() {
		resolution := &types.Resolution{
			Platform:    platform.Platform{OS: "darwin", Arch: "amd64"},
			GitHubAsset: &types.GitHubAsset{AssetName: "faro_darwin_amd64"},
		}
		identity, err := New(WithAssetFilters("faro")).resolveInstallIdentity("mission-control", "0.0.1887", resolution)
		Expect(err).NotTo(HaveOccurred())
		Expect(identity).To(Equal(installIdentity{Name: "faro", AssetName: "faro_darwin_amd64"}))
	})

	It("preserves the executable extension for Windows installs", func() {
		resolution := &types.Resolution{
			Platform:    platform.Platform{OS: "windows", Arch: "amd64"},
			GitHubAsset: &types.GitHubAsset{AssetName: "faro_windows_amd64.exe"},
		}
		identity, err := New(WithAssetFilters("faro")).resolveInstallIdentity("mission-control", "0.0.1887", resolution)
		Expect(err).NotTo(HaveOccurred())
		Expect(identity).To(Equal(installIdentity{Name: "faro.exe", AssetName: "faro_windows_amd64.exe"}))
	})
})
