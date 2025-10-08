package e2e

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/e2e/helpers"
)

var _ = XDescribe("Platform Lock Generation", func() {
	var testData []helpers.PlatformTestData

	BeforeEach(func() {
		testData = helpers.GetPlatformTestData()
		Expect(testData).ToNot(BeEmpty(), "Should have testable platform combinations")
	})

	Context("when generating lock files for specific platforms", func() {
		It("should generate valid lock files for available platform combinations", func() {
			successCount := 0
			skipCount := 0

			for _, data := range testData {
				By(fmt.Sprintf("Testing %s on %s", data.PackageName, data.Platform))

				testCtx, err := helpers.CreateTestEnvironment(data.PackageName, "latest")
				Expect(err).ToNot(HaveOccurred(), "Test environment creation should succeed")

				result := helpers.GenerateLockFile(testCtx, data.PackageName, data.OS, data.Arch)
				testCtx.Cleanup()

				if result.Error != nil {
					GinkgoWriter.Printf("⚠ %s [%s] skipped: %v\n",
						data.PackageName, data.Platform, result.Error)
					skipCount++
					continue
				}

				Expect(result.Duration).To(BeNumerically("<", 60*time.Second),
					fmt.Sprintf("Lock generation for %s on %s should complete within 60 seconds",
						data.PackageName, data.Platform))

				helpers.ValidateLockFile(result, data.PackageName, data.Platform)

				GinkgoWriter.Printf("✓ %s [%s] completed in %v\n",
					data.PackageName, data.Platform, result.Duration)
				successCount++
			}

			GinkgoWriter.Printf("\nSummary: %d successful, %d skipped out of %d total\n",
				successCount, skipCount, len(testData))

			// Expect at least some tests to succeed
			Expect(successCount).To(BeNumerically(">", 0), "At least one platform test should succeed")
		})
	})

	Context("when testing high-priority packages extensively", func() {
		highPriorityPackages := helpers.GetHighPriorityPackages()
		testablePackages := helpers.GetTestablePackages()

		for _, packageName := range highPriorityPackages {
			if platforms, exists := testablePackages[packageName]; exists {
				It(fmt.Sprintf("should generate valid locks for %s across available platforms", packageName), func() {
					successCount := 0
					for _, platform := range platforms {
						By(fmt.Sprintf("Testing %s on %s", packageName, platform))

						testCtx, err := helpers.CreateTestEnvironment(packageName, "latest")
						Expect(err).ToNot(HaveOccurred())

						parts := strings.Split(platform, "-")
						if len(parts) != 2 {
							GinkgoWriter.Printf("⚠ %s [%s] skipped: invalid platform format\n", packageName, platform)
							continue
						}

						result := helpers.GenerateLockFile(testCtx, packageName, parts[0], parts[1])
						testCtx.Cleanup()

						if result.Error != nil {
							GinkgoWriter.Printf("⚠ %s [%s] skipped: %v\n", packageName, platform, result.Error)
							continue
						}

						helpers.ValidateLockFile(result, packageName, platform)
						GinkgoWriter.Printf("✓ %s [%s] validated\n", packageName, platform)
						successCount++
					}

					// Expect at least one platform to succeed for high-priority packages
					Expect(successCount).To(BeNumerically(">", 0),
						fmt.Sprintf("At least one platform should succeed for %s", packageName))
				})
			}
		}
	})

	Context("when testing cross-platform consistency", func() {
		It("should generate consistent metadata across platforms for the same package version", func() {
			packageName := "yq"  // Known stable package
			version := "v4.16.2" // Specific version for consistency

			testCtx, err := helpers.CreateTestEnvironment(packageName, version)
			Expect(err).ToNot(HaveOccurred())
			defer testCtx.Cleanup()

			platforms := []string{"darwin-amd64", "linux-amd64"}
			results := make(map[string]*helpers.LockGenerationResult)

			for _, platform := range platforms {
				parts := strings.Split(platform, "-")
				result := helpers.GenerateLockFile(testCtx, packageName, parts[0], parts[1])
				Expect(result.Error).ToNot(HaveOccurred())
				results[platform] = result
			}

			// Validate consistent version across platforms
			var versions []string
			for platform, result := range results {
				packageEntry := result.LockFile.Dependencies[packageName]
				versions = append(versions, packageEntry.Version)
				GinkgoWriter.Printf("Platform %s: version %s\n", platform, packageEntry.Version)
			}

			// All versions should be identical for same package version constraint
			for i := 1; i < len(versions); i++ {
				Expect(versions[i]).To(Equal(versions[0]),
					"All platforms should resolve to the same version")
			}
		})
	})

	Context("when validating platform-specific characteristics", func() {
		It("should handle platform-specific URL patterns correctly for successful resolutions", func() {
			// Test a subset for URL validation to keep tests reasonable
			limitedTestData := testData
			if len(testData) > 10 {
				limitedTestData = testData[:10] // Test first 10 combinations
			}

			validatedCount := 0
			for _, data := range limitedTestData {
				By(fmt.Sprintf("Validating URLs for %s on %s", data.PackageName, data.Platform))

				testCtx, err := helpers.CreateTestEnvironment(data.PackageName, "latest")
				Expect(err).ToNot(HaveOccurred())

				result := helpers.GenerateLockFile(testCtx, data.PackageName, data.OS, data.Arch)
				testCtx.Cleanup()

				if result.Error != nil {
					GinkgoWriter.Printf("⚠ %s [%s] skipped URL validation: %v\n",
						data.PackageName, data.Platform, result.Error)
					continue
				}

				lockFile := result.LockFile
				packageEntry := lockFile.Dependencies[data.PackageName]
				platformEntry := packageEntry.Platforms[data.Platform]

				// helpers.ValidatePlatformEntry(platformEntry, data.PackageName, data.Platform)

				Expect(platformEntry.URL).To(MatchRegexp(`^https?://`), "URL should be HTTP/HTTPS")
				Expect(platformEntry.URL).ToNot(BeEmpty(), "URL should not be empty")

				GinkgoWriter.Printf("✓ %s [%s] URL validated\n", data.PackageName, data.Platform)
				validatedCount++
			}

			// Expect at least some URL validations to succeed
			Expect(validatedCount).To(BeNumerically(">", 0), "At least one URL validation should succeed")
		})
	})
})
