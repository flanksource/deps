//go:build darwin

package system

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// buildImage makes a real disk image holding the named files, so the hdiutil
// interaction is exercised rather than mocked — a stubbed mount would not catch
// a wrong flag, and the flags are the whole of what mountImage does.
func buildImage(name string, contents ...string) string {
	source := GinkgoT().TempDir()
	for _, file := range contents {
		Expect(os.WriteFile(filepath.Join(source, file), []byte("payload"), 0o644)).To(Succeed())
	}

	image := filepath.Join(GinkgoT().TempDir(), name+".dmg")
	create := exec.Command("hdiutil", "create",
		"-volname", name, "-srcfolder", source, "-ov", "-format", "UDZO", image)
	output, err := create.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "hdiutil create: %s", output)

	return image
}

var _ = Describe("disk images", Label("darwin"), func() {
	Describe("mountImage", func() {
		It("mounts an image read-only and releases it again", func() {
			const payload = "cinc-auditor-7.1.7-1.arm64.pkg"
			image := buildImage("deps-test", payload)

			mountPoint, unmount, err := mountImage(image)
			Expect(err).ToNot(HaveOccurred())

			Expect(filepath.Join(mountPoint, payload)).To(BeAnExistingFile())

			// Read-only: the installer must not be able to alter the artifact
			// whose checksum was just verified.
			Expect(os.WriteFile(filepath.Join(mountPoint, "scratch"), []byte("x"), 0o644)).ToNot(Succeed())

			Expect(unmount()).To(Succeed())
			Expect(mountPoint).ToNot(BeADirectory())
		})

		It("reports a file that is not a disk image", func() {
			notAnImage := filepath.Join(GinkgoT().TempDir(), "notes.dmg")
			Expect(os.WriteFile(notAnImage, []byte("not a disk image"), 0o644)).To(Succeed())

			_, _, err := mountImage(notAnImage)

			Expect(err).To(MatchError(ContainSubstring("failed to mount notes.dmg")))
		})
	})

	Describe("mount and discover together", func() {
		It("finds the package inside a real image", func() {
			// This is the shape CINC and Chef ship on macOS: one .pkg at the
			// root of the image, which is what InstallDmg hands to InstallPkg.
			const payload = "cinc-auditor-7.1.7-1.arm64.pkg"
			image := buildImage("deps-test-single", payload)

			mountPoint, unmount, err := mountImage(image)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(unmount)

			found, err := findPackageInImage(mountPoint)

			Expect(err).ToNot(HaveOccurred())
			Expect(filepath.Base(found)).To(Equal(payload))
		})

		It("refuses an image carrying more than one package", func() {
			image := buildImage("deps-test-multi", "tool-core.pkg", "tool-extras.pkg")

			mountPoint, unmount, err := mountImage(image)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(unmount)

			_, err = findPackageInImage(mountPoint)

			Expect(err).To(MatchError(ContainSubstring("cannot tell which one")))
		})
	})
})
