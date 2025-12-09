package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps"
	"github.com/flanksource/deps/e2e/helpers"
)

var _ = Describe("Installation tests", func() {
	arch := os.Getenv("TEST_ARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}
	testOS := os.Getenv("TEST_OS")
	if testOS == "" {
		testOS = runtime.GOOS
	}
	Describe(testOS, func() {
		var testCtx *helpers.TestContext

		BeforeEach(func() {
			var err error
			testCtx, err = helpers.CreateInstallTestEnvironment()
			Expect(err).ToNot(HaveOccurred(), "Test environment creation should succeed")
		})

		AfterEach(func() {
			if testCtx != nil {
				testCtx.Cleanup()
			}
		})

		for _, packageData := range helpers.GetPackagesToTest(testOS, arch) {

			It(fmt.Sprintf("should install %s on %s", packageData.PackageName, packageData.Platform), func() {

				tempDir, err := os.MkdirTemp("", "deps-e2e-"+packageData.PackageName+"-*")
				Expect(err).ToNot(HaveOccurred(), "failed to create temp dir")

				result, err := deps.Install(packageData.PackageName, "stable",
					deps.WithOS(testOS, arch),
					deps.WithAppDir(filepath.Join(tempDir, "app")),
					deps.WithBinDir(filepath.Join(tempDir, "bin")))

				Expect(err).ToNot(HaveOccurred(), "Installation should not error")
				if result != nil {
					GinkgoWriter.Printf("%s\n", result.Pretty().ANSI())
				}
			})
		}

		It("should install flux", func() {
			tempDir, err := os.MkdirTemp("", "")
			Expect(err).ToNot(HaveOccurred(), "failed to create temp dir")

			result, err := deps.Install("fluxcd/flux2", "stable",
				deps.WithOS(testOS, arch),
				deps.WithAppDir(filepath.Join(tempDir, "app")),
				deps.WithBinDir(filepath.Join(tempDir, "bin")))
			Expect(err).ToNot(HaveOccurred(), "Installation should not error")
			if result != nil {
				GinkgoWriter.Printf("%s\n", result.Pretty().ANSI())
			}
		})
		It("should install stern", func() {
			tempDir, err := os.MkdirTemp("", "")
			Expect(err).ToNot(HaveOccurred(), "failed to create temp dir")

			result, err := deps.Install("stern/stern", "stable",
				deps.WithOS(testOS, arch),
				deps.WithAppDir(filepath.Join(tempDir, "app")),
				deps.WithBinDir(filepath.Join(tempDir, "bin")))
			Expect(err).ToNot(HaveOccurred(), "Installation should not error")
			if result != nil {
				GinkgoWriter.Printf("%s\n", result.Pretty().ANSI())
			}
		})
	})

})
