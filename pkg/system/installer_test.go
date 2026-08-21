package system

import (
	"os"
	"runtime"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "System Installer Suite")
}

var _ = Describe("system installers", func() {
	Describe("GetSystemInstallerType", func() {
		DescribeTable("routes an artifact to the installer that understands it",
			func(name, expected string) {
				Expect(GetSystemInstallerType(name)).To(Equal(expected))
			},
			Entry("macos package", "AWSCLIV2-2.15.0.pkg", "macos_installer"),
			Entry("macos disk image", "cinc-auditor-7.1.7-1.arm64.dmg", "macos_disk_image"),
			Entry("windows installer", "tool-1.0.0.msi", "windows_installer"),
			Entry("debian package", "cinc-auditor_7.1.7-1_amd64.deb", "debian_package"),
			Entry("an archive is not a system installer", "helm-v3.16.2.tar.gz", "unknown"),
			Entry("a bare binary is not a system installer", "jq-linux-amd64", "unknown"),
		)

		It("is case insensitive", func() {
			// Vendors are inconsistent about casing in artifact filenames, and
			// misrouting one means it is treated as a bare binary and copied
			// into bin-dir unrunnable.
			Expect(GetSystemInstallerType("Installer.DMG")).To(Equal("macos_disk_image"))
			Expect(GetSystemInstallerType("Package.DEB")).To(Equal("debian_package"))
		})
	})

	Describe("InstallSystemPackage", func() {
		It("refuses an artifact that is not a system installer", func() {
			_, err := InstallSystemPackage("helm-v3.16.2.tar.gz", "", &SystemInstallOptions{Silent: true})

			Expect(err).To(MatchError(ContainSubstring("unsupported installer type")))
		})

		// Each installer shells out to a platform tool — installer, hdiutil,
		// msiexec, dpkg — so the wrong-platform guard is what turns an
		// unsupported combination into a clear error instead of a missing
		// command.
		It("refuses a .deb off Linux", func() {
			if runtime.GOOS == "linux" {
				Skip("the guard under test only rejects on non-Linux hosts")
			}

			_, err := InstallSystemPackage("cinc-auditor_7.1.7-1_amd64.deb", "", &SystemInstallOptions{Silent: true})

			Expect(err).To(MatchError(ContainSubstring("only be installed on Linux")))
		})

		It("refuses a .dmg off macOS", func() {
			if runtime.GOOS == "darwin" {
				Skip("the guard under test only rejects on non-macOS hosts")
			}

			_, err := InstallSystemPackage("cinc-auditor-7.1.7-1.arm64.dmg", "", &SystemInstallOptions{Silent: true})

			Expect(err).To(MatchError(ContainSubstring("only be mounted on macOS")))
		})
	})

	Describe("findPackageInImage", func() {
		It("reports an image holding no package", func() {
			_, err := findPackageInImage(GinkgoT().TempDir())

			Expect(err).To(MatchError(ContainSubstring("no .pkg found")))
		})

		It("finds the single package in an image", func() {
			mount := GinkgoT().TempDir()
			expected := mount + "/cinc-auditor-7.1.7-1.arm64.pkg"
			Expect(writeFile(expected)).To(Succeed())

			found, err := findPackageInImage(mount)

			Expect(err).ToNot(HaveOccurred())
			Expect(found).To(Equal(expected))
		})

		It("refuses to guess between several packages", func() {
			// Which package a multi-package image expects you to install is not
			// something the filenames say, so picking one would install the
			// wrong software rather than fail.
			mount := GinkgoT().TempDir()
			Expect(writeFile(mount + "/tool-core.pkg")).To(Succeed())
			Expect(writeFile(mount + "/tool-extras.pkg")).To(Succeed())

			_, err := findPackageInImage(mount)

			Expect(err).To(MatchError(ContainSubstring("cannot tell which one")))
		})
	})
})

func writeFile(path string) error {
	return os.WriteFile(path, []byte("stub"), 0o644)
}
