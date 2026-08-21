package extract

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestExtract(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Extract Suite")
}

var _ = Describe("artifact classification", func() {
	// An artifact is one of three things, and the three answers have to agree:
	// something unpacked into bin-dir, an operating-system package handed to
	// the system installer, or a bare binary. A file classified as both an
	// archive and an installer, or as neither, is installed wrongly.
	DescribeTable("recognises what an artifact is",
		func(name string, wantExtension string, wantArchive, wantInstaller bool) {
			Expect(GetExtension(name)).To(Equal(wantExtension), "extension")
			Expect(IsArchive(name)).To(Equal(wantArchive), "archive")
			Expect(IsSystemInstaller(name)).To(Equal(wantInstaller), "system installer")
		},
		Entry("gzipped tarball", "helm-v3.16.2-linux-amd64.tar.gz", ".tar.gz", true, false),
		Entry("zip", "tlsx_1.3.0_linux_amd64.zip", ".zip", true, false),
		Entry("xz tarball", "dcg-aarch64-unknown-linux-gnu.tar.xz", ".tar.xz", true, false),

		// Omnibus packages: only runnable at the absolute prefix they were
		// built for, so never unpacked into bin-dir.
		Entry("debian package", "cinc-auditor_7.1.7-1_amd64.deb", ".deb", false, true),
		Entry("macos disk image", "cinc-auditor-7.1.7-1.arm64.dmg", ".dmg", false, true),
		Entry("macos package", "AWSCLIV2-2.15.0.pkg", ".pkg", false, true),
		Entry("windows installer", "tool-1.0.0.msi", ".msi", false, true),

		Entry("bare binary", "jq-linux-amd64", "", false, false),
	)

	It("classifies from a URL with query parameters", func() {
		// Download URLs carry signed-request parameters; the extension has to
		// survive them or the file is downloaded without one and then fails to
		// be recognised as an installer.
		const signed = "https://packages.example.com/cinc-auditor_7.1.7-1_amd64.deb?token=abc123"

		Expect(GetExtension(signed)).To(Equal(".deb"))
	})

	Describe("IsSystemInstallerExtension", func() {
		It("agrees with IsSystemInstaller for a bare extension", func() {
			// The download step names its temporary file from the extension
			// alone, before there is a path to test. The two answers drifting
			// apart means an installer downloaded without its extension.
			for _, ext := range []string{".pkg", ".dmg", ".msi", ".deb"} {
				Expect(IsSystemInstallerExtension(ext)).To(BeTrue(), ext)
				Expect(IsSystemInstaller("artifact"+ext)).To(BeTrue(), ext)
			}
			for _, ext := range []string{".tar.gz", ".zip", ""} {
				Expect(IsSystemInstallerExtension(ext)).To(BeFalse(), ext)
			}
		})

		It("is case insensitive", func() {
			Expect(IsSystemInstallerExtension(".DEB")).To(BeTrue())
			Expect(IsSystemInstallerExtension(".DMG")).To(BeTrue())
		})
	})
})
