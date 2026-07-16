package github

import (
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GitHubReleaseManager binary path", func() {
	It("provides the resolved version to CEL expressions", func() {
		manager := NewGitHubReleaseManager()
		pkg := types.Package{
			Name:       "rclone",
			BinaryPath: `"rclone-v" + version + "-" + os + "-" + arch + "/rclone" + (os == "windows" ? ".exe" : "")`,
		}
		plat := platform.Platform{OS: "windows", Arch: "amd64"}

		path := manager.guessBinaryPath(pkg, "rclone-v1.74.4-windows-amd64.zip", "1.74.4", plat)

		Expect(path).To(Equal("rclone-v1.74.4-windows-amd64/rclone.exe"))
	})
})
