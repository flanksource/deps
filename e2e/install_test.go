package e2e

import (
	"fmt"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/e2e/helpers"
)

var _ = Describe("Installation tests", func() {
	platforms := helpers.GetPlatformsForTesting()

	for _, platform := range platforms {
		Describe(platform, func() {
			var testCtx *helpers.TestContext
			var allInstallData []helpers.InstallTestData
			var platformData []helpers.InstallTestData
			var supportedPackages []helpers.InstallTestData
			var unsupportedPackages []helpers.InstallTestData
			currentPlatform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)

			testCtx, err := helpers.CreateInstallTestEnvironment()
			Expect(err).ToNot(HaveOccurred(), "Test environment creation should succeed")

			// Get all test data and filter for this platform
			allInstallData = helpers.GetAllDependenciesInstallData()
			platformData = []helpers.InstallTestData{}
			supportedPackages = []helpers.InstallTestData{}
			unsupportedPackages = []helpers.InstallTestData{}

			for _, data := range allInstallData {
				if data.Platform == platform {
					platformData = append(platformData, data)
					if data.IsSupported {
						supportedPackages = append(supportedPackages, data)
					} else {
						unsupportedPackages = append(unsupportedPackages, data)
					}
				}
			}

			GinkgoWriter.Printf("Testing %d packages on %s (%d unsupported)\n",
				len(supportedPackages), platform, len(unsupportedPackages))
			AfterEach(func() {
				if testCtx != nil {
					testCtx.Cleanup()
				}
			})

			It("should have dependencies to test", func() {
				Expect(platformData).ToNot(BeEmpty(),
					fmt.Sprintf("Should have dependencies to test on %s", platform))
			})

			for _, data := range supportedPackages {
				// Capture the loop variable for the closure
				packageData := data
				It(packageData.PackageName, func() {

					result := helpers.TestInstallation(testCtx, packageData.PackageName, packageData.Version, packageData.OS, packageData.Arch)

					if result.Error != nil {
						Fail(fmt.Sprintf("%s [%s] installation failed: %v",
							packageData.PackageName, packageData.Platform, result.Error))
					}

					// Validate the installation
					err := helpers.ValidateInstalledBinary(result, packageData.PackageName, packageData.OS, packageData.Arch)
					if err != nil {
						Fail(fmt.Sprintf("%s [%s] validation failed: %v",
							packageData.PackageName, packageData.Platform, err))
					}

					versionMsg := ""
					if packageData.Platform == currentPlatform {
						versionMsg = " (version validated)"
					} else {
						versionMsg = " (cross-platform)"
					}

					GinkgoWriter.Printf("✓ %s [%s] installed in %v%s\n",
						packageData.PackageName, packageData.Platform, result.Duration, versionMsg)
				})
			}
		})
	}
})
