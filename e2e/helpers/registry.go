package helpers

import (
	"sort"
	"strings"

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/types"
)

// PlatformTestData represents a test case for a specific package-platform combination
type PlatformTestData struct {
	PackageName   string
	Platform      string
	ExpectedAsset string
	Manager       string
	OS            string
	Arch          string
}

// InstallTestData represents installation test case for package-platform combination
type InstallTestData struct {
	PackageName string
	Platform    string
	OS          string
	Arch        string
	Version     string
	Manager     string
	IsSupported bool
}

// GetTestablePackages returns a map of package names to their supported platforms
// Only includes packages that have multiple platform support for meaningful testing
func GetTestablePackages() map[string][]string {
	registry := config.GetGlobalRegistry()
	testablePackages := make(map[string][]string)

	for packageName, pkg := range registry.Registry {
		platforms := ExtractSupportedPlatforms(pkg)

		// Only include packages with multiple platforms or specific test-worthy characteristics
		if len(platforms) >= 2 || isTestWorthyPackage(packageName, pkg) {
			// Double-check we haven't excluded this package
			if !isExcludedPackage(packageName) {
				testablePackages[packageName] = platforms
			}
		}
	}

	return testablePackages
}

// isExcludedPackage checks if a package should be excluded from testing
func isExcludedPackage(packageName string) bool {
	excludedPackages := []string{
		"ketall", // Binary extraction/validation issues
		"go",     // Direct URL manager requires explicit version
	}
	return contains(excludedPackages, packageName)
}

// GetHighPriorityPackages returns packages that should be tested more extensively
func GetHighPriorityPackages() []string {
	return []string{
		"helm",    // Kubernetes package manager - widely used
		"kubectl", // Kubernetes CLI - critical tool
		"kind",    // Local Kubernetes - CI/CD tool
		"yq",      // YAML processor - common utility
		"jq",      // JSON processor - common utility
	}
}

// GetPlatformTestData generates all test data for platform-specific lock tests
func GetPlatformTestData() []PlatformTestData {
	var testData []PlatformTestData
	testablePackages := GetTestablePackages()
	highPriorityPackages := GetHighPriorityPackages()

	registry := config.GetGlobalRegistry()

	for packageName, platforms := range testablePackages {
		pkg := registry.Registry[packageName]
		isHighPriority := contains(highPriorityPackages, packageName)

		for _, platform := range platforms {
			// For high priority packages, test all platforms
			// For others, test a subset to keep test time reasonable
			if isHighPriority || shouldTestPlatform(platform) {
				parts := strings.Split(platform, "-")
				if len(parts) != 2 {
					continue // Skip malformed platform strings
				}

				testData = append(testData, PlatformTestData{
					PackageName:   packageName,
					Platform:      platform,
					ExpectedAsset: getExpectedAsset(pkg, platform),
					Manager:       pkg.Manager,
					OS:            parts[0],
					Arch:          parts[1],
				})
			}
		}
	}

	// Sort for consistent test ordering
	sort.Slice(testData, func(i, j int) bool {
		if testData[i].PackageName == testData[j].PackageName {
			return testData[i].Platform < testData[j].Platform
		}
		return testData[i].PackageName < testData[j].PackageName
	})

	return testData
}

// ExtractSupportedPlatforms extracts platform support from a package definition
// This is based on the logic from cmd/list.go but adapted for testing
func ExtractSupportedPlatforms(pkg types.Package) []string {
	platforms := make(map[string]bool)

	// Extract from asset patterns
	for pattern := range pkg.AssetPatterns {
		// Handle patterns like "linux-*", "darwin-*,windows-*"
		if strings.Contains(pattern, "*") {
			// Extract base patterns
			parts := strings.Split(pattern, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasSuffix(part, "-*") {
					// Add common architectures for wildcard patterns
					base := strings.TrimSuffix(part, "-*")
					if base == "linux" || base == "darwin" || base == "windows" {
						platforms[base+"-amd64"] = true
						platforms[base+"-arm64"] = true
					}
				} else {
					platforms[part] = true
				}
			}
		} else {
			platforms[strings.TrimSpace(pattern)] = true
		}
	}

	// Handle URL template based packages (kubectl, terraform, etc.)
	if pkg.URLTemplate != "" {
		// These typically support many platforms via template variables
		// Add common combinations for testing
		commonPlatforms := []string{
			"darwin-amd64", "darwin-arm64",
			"linux-amd64", "linux-arm64",
			"windows-amd64",
		}
		for _, platform := range commonPlatforms {
			platforms[platform] = true
		}
	}

	// Convert to sorted slice
	result := make([]string, 0, len(platforms))
	for platform := range platforms {
		result = append(result, platform)
	}
	sort.Strings(result)

	return result
}

// isTestWorthyPackage determines if a package should be included even with limited platform support
func isTestWorthyPackage(packageName string, pkg types.Package) bool {
	// Include packages that are commonly used or demonstrate specific manager types
	commonPackages := []string{
		"jq",        // Limited platforms but very common
		"postgres",  // Maven-based package
		"terraform", // Direct URL with checksum
		"sops",      // Security tool
	}

	// Exclude known problematic packages that consistently fail
	problematicPackages := []string{
		"actions-runner", // Asset resolution issues (fixed but keeping exclusion for now)
		"ketall",         // Binary extraction/validation issues
		"go",             // Direct URL manager requires explicit version
	}

	if contains(problematicPackages, packageName) {
		return false
	}

	return contains(commonPackages, packageName)
}

// shouldTestPlatform determines which platforms to test for non-high-priority packages
func shouldTestPlatform(platform string) bool {
	// Focus on most common platforms to keep test suite manageable
	commonPlatforms := []string{
		"darwin-amd64",
		"linux-amd64",
		"darwin-arm64", // Growing importance of ARM64
	}

	return contains(commonPlatforms, platform)
}

// getExpectedAsset returns the expected asset pattern for a package-platform combination
func getExpectedAsset(pkg types.Package, platform string) string {
	if asset, exists := pkg.AssetPatterns[platform]; exists {
		return asset
	}

	// For URL template based packages, return template info
	if pkg.URLTemplate != "" {
		return "url_template"
	}

	return "unknown"
}

// GetPlatformsForTesting returns test platforms for installation testing
func GetPlatformsForTesting() []string {
	return []string{
		"linux-amd64",
		"linux-arm64",
		"darwin-amd64",
		"darwin-arm64",
		"windows-amd64",
	}
}

// GetLinuxPlatformsForTesting returns only Linux platforms for full installation testing
func GetLinuxPlatformsForTesting() []string {
	return []string{
		"linux-amd64",
		"linux-arm64",
	}
}

// GetChecksumOnlyPlatformsForTesting returns platforms for checksum-only testing
func GetChecksumOnlyPlatformsForTesting() []string {
	return []string{
		"darwin-amd64",
		"darwin-arm64",
		"windows-amd64",
	}
}

// GetAllDependenciesInstallData generates test data for ALL packages in the default registry
func GetAllDependenciesInstallData() []InstallTestData {
	var testData []InstallTestData
	registry := config.GetGlobalRegistry()
	platforms := GetPlatformsForTesting()

	// Generate test data for every package in the registry
	for packageName, pkg := range registry.Registry {
		// Skip excluded packages
		if isExcludedPackage(packageName) {
			continue
		}

		supportedPlatforms := ExtractSupportedPlatforms(pkg)

		// Create test data for each platform we want to test
		for _, platform := range platforms {
			parts := strings.Split(platform, "-")
			if len(parts) != 2 {
				continue // Skip malformed platform strings
			}

			isSupported := contains(supportedPlatforms, platform)

			// Include all packages, both supported and unsupported (for skip tests)
			testData = append(testData, InstallTestData{
				PackageName: packageName,
				Platform:    platform,
				OS:          parts[0],
				Arch:        parts[1],
				Version:     "stable", // Use latest for testing
				Manager:     pkg.Manager,
				IsSupported: isSupported,
			})
		}
	}

	// Sort for consistent test ordering
	sort.Slice(testData, func(i, j int) bool {
		if testData[i].Platform == testData[j].Platform {
			return testData[i].PackageName < testData[j].PackageName
		}
		return testData[i].Platform < testData[j].Platform
	})

	return testData
}

// contains checks if a string slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
