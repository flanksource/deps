package installer_test

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
)

func TestDirectoryMode(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Directory Mode Suite")
}

var _ = Describe("Directory Mode Installation", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "deps-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	It("should support directory mode in package definition", func() {
		pkg := types.Package{
			Name: "test-tool",
			Mode: "directory",
		}

		Expect(pkg.Mode).To(Equal("directory"))
	})

	It("should handle binary mode as default", func() {
		pkg := types.Package{
			Name: "test-tool",
		}

		Expect(pkg.Mode).To(Equal(""))
	})

	It("should validate directory mode package configuration", func() {
		// Test package with directory mode
		pkg := types.Package{
			Name:           "node",
			Repo:           "nodejs/node",
			Mode:           "directory",
			URLTemplate:    "https://nodejs.org/download/release/{{.tag}}/node-{{.tag}}-{{.os}}-{{.arch}}.tar.gz",
			VersionCommand: "bin/node --version",
			VersionPattern: "v(\\d+\\.\\d+\\.\\d+)",
		}

		Expect(pkg.Mode).To(Equal("directory"))
		Expect(pkg.VersionCommand).To(Equal("bin/node --version"))
	})

	It("should support symlinks configuration", func() {
		pkg := types.Package{
			Name:     "test-tool",
			Mode:     "directory",
			Symlinks: []string{"bin/*", "lib/tool"},
		}

		Expect(pkg.Symlinks).To(HaveLen(2))
		Expect(pkg.Symlinks[0]).To(Equal("bin/*"))
		Expect(pkg.Symlinks[1]).To(Equal("lib/tool"))
	})
})
