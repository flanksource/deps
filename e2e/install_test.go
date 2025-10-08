package e2e

import (
	"fmt"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/e2e/helpers"
)

var _ = Describe("Installation tests", func() {
	platforms := helpers.GetLinuxPlatformsForTesting()

	for _, platform := range platforms {
		platform := platform // capture loop variable
		Describe(platform, func() {
			var testCtx *helpers.TestContext
			currentPlatform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)

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

			It("should install and validate all supported packages", func() {
				allInstallData := helpers.GetAllDependenciesInstallData()
				supportedPackages := []helpers.InstallTestData{}

				for _, data := range allInstallData {
					if data.Platform == platform && data.IsSupported {
						supportedPackages = append(supportedPackages, data)
					}
				}

				Expect(supportedPackages).ToNot(BeEmpty(),
					fmt.Sprintf("Should have supported packages to test on %s", platform))

				GinkgoWriter.Printf("Testing %d packages on %s\n",
					len(supportedPackages), platform)

				successCount := 0
				failureCount := 0

				for _, packageData := range supportedPackages {
					result := helpers.TestInstallation(testCtx, packageData.PackageName, packageData.Version, packageData.OS, packageData.Arch)

					if result.Error != nil {
						GinkgoWriter.Printf("✗ %s [%s] installation failed: %v\n",
							packageData.PackageName, packageData.Platform, result.Error)
						failureCount++
						continue
					}

					// Validate the installation
					err := helpers.ValidateInstalledBinary(result, packageData.PackageName, packageData.OS, packageData.Arch)
					if err != nil {
						GinkgoWriter.Printf("✗ %s [%s] validation failed: %v\n",
							packageData.PackageName, packageData.Platform, err)
						failureCount++
						continue
					}

					versionMsg := ""
					if packageData.Platform == currentPlatform {
						versionMsg = " (version validated)"
					} else {
						versionMsg = " (cross-platform)"
					}

					GinkgoWriter.Printf("✓ %s [%s] installed in %v%s\n",
						packageData.PackageName, packageData.Platform, result.Duration, versionMsg)
					successCount++
				}

				GinkgoWriter.Printf("\nSummary for %s: %d successful, %d failed out of %d total\n",
					platform, successCount, failureCount, len(supportedPackages))

				// Require at least 80% success rate
				successRate := float64(successCount) / float64(len(supportedPackages))
				Expect(successRate).To(BeNumerically(">=", 0.8),
					fmt.Sprintf("At least 80%% of packages should install successfully on %s", platform))
			})
		})
	}
})
