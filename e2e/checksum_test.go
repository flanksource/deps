package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/e2e/helpers"
)

var _ = Describe("Checksum validation tests", func() {
	platforms := helpers.GetChecksumOnlyPlatformsForTesting()

	for _, platform := range platforms {
		Describe(platform, func() {
			var testCtx *helpers.TestContext
			var allInstallData []helpers.InstallTestData
			var platformData []helpers.InstallTestData
			var supportedPackages []helpers.InstallTestData

			testCtx, err := helpers.CreateInstallTestEnvironment()
			Expect(err).ToNot(HaveOccurred(), "Test environment creation should succeed")

			// Get all test data and filter for this platform
			allInstallData = helpers.GetAllDependenciesInstallData()
			platformData = []helpers.InstallTestData{}
			supportedPackages = []helpers.InstallTestData{}

			for _, data := range allInstallData {
				if data.Platform == platform {
					platformData = append(platformData, data)
					if data.IsSupported {
						supportedPackages = append(supportedPackages, data)
					}
				}
			}

			GinkgoWriter.Printf("Testing checksums for %d packages on %s\n",
				len(supportedPackages), platform)

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

					result := helpers.TestChecksumValidation(testCtx, packageData.PackageName, packageData.Version, packageData.OS, packageData.Arch)

					if result.Error != nil {
						Fail(fmt.Sprintf("%s [%s] checksum validation failed: %v",
							packageData.PackageName, packageData.Platform, result.Error))
					}

					// Validate the checksum result
					err := helpers.ValidateChecksumResult(result, packageData.PackageName, packageData.OS, packageData.Arch)
					if err != nil {
						Fail(fmt.Sprintf("%s [%s] checksum result validation failed: %v",
							packageData.PackageName, packageData.Platform, err))
					}

					GinkgoWriter.Printf("✓ %s [%s] checksum validated in %v\n",
						packageData.PackageName, packageData.Platform, result.Duration)
				})
			}
		})
	}
})
