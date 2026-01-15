package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps"
	"github.com/flanksource/deps/e2e/helpers"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/verify"
)

// verifyBinariesInDir finds all executables in binDir and verifies they match expected OS/arch
func verifyBinariesInDir(binDir, expectedOS, expectedArch string) error {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return fmt.Errorf("failed to read bin dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// Skip marker files
		name := entry.Name()
		if strings.HasSuffix(name, ".installed") {
			continue
		}
		binaryPath := filepath.Join(binDir, name)

		// Check if it's a symlink and resolve it
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(binaryPath)
			if err != nil {
				continue
			}
			binaryPath = resolved
		}

		// Detect binary info
		binaryInfo, err := verify.DetectBinaryPlatform(binaryPath)
		if err != nil {
			return fmt.Errorf("binary %s: %w", name, err)
		}

		// Skip unknown format (shell scripts, Java wrappers, etc.) and dotnet assemblies
		if binaryInfo.Type == "unknown" || binaryInfo.Type == "dotnet" {
			GinkgoWriter.Printf("Skipping %s (%s - not a native binary)\n", name, binaryInfo.Type)
			continue
		}

		// Verify platform matches
		if binaryInfo.OS != expectedOS {
			return fmt.Errorf("binary %s: OS mismatch: expected %s, got %s", name, expectedOS, binaryInfo.OS)
		}
		if binaryInfo.Arch != expectedArch {
			return fmt.Errorf("binary %s: arch mismatch: expected %s, got %s", name, expectedArch, binaryInfo.Arch)
		}
	}
	return nil
}

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

		// Set global platform overrides from CLI flags
		platform.SetGlobalOverrides(testOS, arch)

		for _, packageData := range helpers.GetPackagesToTest(testOS, arch) {

			It(fmt.Sprintf("should install %s on %s", packageData.PackageName, packageData.Platform), func() {

				tempDir, err := os.MkdirTemp("", "deps-e2e-"+packageData.PackageName+"-*")
				Expect(err).ToNot(HaveOccurred(), "failed to create temp dir")

				binDir := filepath.Join(tempDir, "bin")
				result, err := deps.Install(packageData.PackageName, "stable",
					deps.WithOS(testOS, arch),
					deps.WithAppDir(filepath.Join(tempDir, "app")),
					deps.WithBinDir(binDir))

				Expect(err).ToNot(HaveOccurred(), "Installation should not error")
				if result != nil {
					GinkgoWriter.Printf("%s\n", result.Pretty().ANSI())
				}

				// Verify installed binaries match expected platform
				err = verifyBinariesInDir(binDir, testOS, arch)
				Expect(err).ToNot(HaveOccurred(), "Binary platform verification should pass")
			})
		}

		It("should install flux", func() {
			tempDir, err := os.MkdirTemp("", "")
			Expect(err).ToNot(HaveOccurred(), "failed to create temp dir")

			binDir := filepath.Join(tempDir, "bin")
			result, err := deps.Install("fluxcd/flux2", "stable",
				deps.WithOS(testOS, arch),
				deps.WithAppDir(filepath.Join(tempDir, "app")),
				deps.WithBinDir(binDir))
			Expect(err).ToNot(HaveOccurred(), "Installation should not error")
			if result != nil {
				GinkgoWriter.Printf("%s\n", result.Pretty().ANSI())
			}

			// Verify installed binaries match expected platform
			err = verifyBinariesInDir(binDir, testOS, arch)
			Expect(err).ToNot(HaveOccurred(), "Binary platform verification should pass")
		})
		It("should install stern", func() {
			tempDir, err := os.MkdirTemp("", "")
			Expect(err).ToNot(HaveOccurred(), "failed to create temp dir")

			binDir := filepath.Join(tempDir, "bin")
			result, err := deps.Install("stern/stern", "stable",
				deps.WithOS(testOS, arch),
				deps.WithAppDir(filepath.Join(tempDir, "app")),
				deps.WithBinDir(binDir))
			Expect(err).ToNot(HaveOccurred(), "Installation should not error")
			if result != nil {
				GinkgoWriter.Printf("%s\n", result.Pretty().ANSI())
			}

			// Verify installed binaries match expected platform
			err = verifyBinariesInDir(binDir, testOS, arch)
			Expect(err).ToNot(HaveOccurred(), "Binary platform verification should pass")
		})
	})

})
