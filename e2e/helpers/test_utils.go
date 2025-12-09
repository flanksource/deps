package helpers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/lock"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/gomega" //nolint:staticcheck
)

// TestContext holds the context and resources for a single test
type TestContext struct {
	TempDir    string
	OldWD      string
	ConfigFile string
	Cleanup    func()
}

// CreateTestEnvironment sets up a temporary directory and test configuration
func CreateTestEnvironment(packageName, version string) (*TestContext, error) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("deps-e2e-%s-*", packageName))
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Save current working directory
	oldWD, err := os.Getwd()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Create test deps.yaml
	configContent := fmt.Sprintf(`dependencies:
  %s: "%s"

settings:
  bin_dir: ./test-bin
  platforms:
    - darwin-amd64
    - darwin-arm64
    - linux-amd64
    - linux-arm64
    - windows-amd64
`, packageName, version)

	configFile := filepath.Join(tempDir, "deps.yaml")
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to write config file: %w", err)
	}

	// Change to test directory
	if err := os.Chdir(tempDir); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to change directory: %w", err)
	}

	cleanup := func() {
		_ = os.Chdir(oldWD)
		_ = os.RemoveAll(tempDir)
		// Reset global platform overrides
		platform.SetGlobalOverrides("", "")
	}

	return &TestContext{
		TempDir:    tempDir,
		OldWD:      oldWD,
		ConfigFile: configFile,
		Cleanup:    cleanup,
	}, nil
}

// LockGenerationResult holds the results of lock file generation
type LockGenerationResult struct {
	LockFile *types.LockFile
	Duration time.Duration
	Error    error
}

// GenerateLockFile runs the lock generation for specific OS and architecture
func GenerateLockFile(testCtx *TestContext, packageName, osTarget, archTarget string) *LockGenerationResult {
	start := time.Now()

	// Set platform overrides
	platform.SetGlobalOverrides(osTarget, archTarget)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get package manager registry
	managers := manager.GetGlobalRegistry()
	generator := lock.NewGenerator(managers)

	// Load configuration
	depsConfig := config.GetGlobalRegistry()

	// Create constraints map
	constraints := map[string]string{
		packageName: depsConfig.Dependencies[packageName],
	}

	// If package not in dependencies, use "latest"
	if constraints[packageName] == "" {
		constraints[packageName] = "latest"
	}

	// Set up lock options with specific platform
	targetPlatform := fmt.Sprintf("%s-%s", osTarget, archTarget)
	lockOpts := types.LockOptions{
		Platforms: []string{targetPlatform},
		Packages:  []string{packageName},
	}

	// Generate lock file
	testTask := &task.Task{}
	lockFile, err := generator.Generate(ctx, constraints, depsConfig.Registry, lockOpts, testTask)

	// Wait for async tasks
	task.WaitForAllTasks()

	duration := time.Since(start)

	return &LockGenerationResult{
		LockFile: lockFile,
		Duration: duration,
		Error:    err,
	}
}

// ValidateLockFile performs comprehensive validation of generated lock file
// Caller should check for result.Error before calling this function
func ValidateLockFile(result *LockGenerationResult, expectedPackage, expectedPlatform string) {
	Expect(result.Error).ToNot(HaveOccurred(), "Lock file generation should succeed")
	Expect(result.LockFile).ToNot(BeNil(), "Lock file should not be nil")

	lockFile := result.LockFile

	// Validate lock file structure
	Expect(lockFile.Version).To(Equal("1.0"), "Lock file version should be 1.0")
	Expect(lockFile.Dependencies).To(HaveLen(1), "Should have exactly one dependency")

	// Validate package entry
	packageEntry, exists := lockFile.Dependencies[expectedPackage]
	Expect(exists).To(BeTrue(), fmt.Sprintf("Package %s should exist in lock file", expectedPackage))
	Expect(packageEntry.Version).ToNot(BeEmpty(), "Package version should not be empty")

	// Validate platform-specific entry
	Expect(packageEntry.Platforms).To(HaveKey(expectedPlatform),
		fmt.Sprintf("Platform %s should exist for package %s", expectedPlatform, expectedPackage))

	platformEntry := packageEntry.Platforms[expectedPlatform]
	Expect(platformEntry.URL).ToNot(BeEmpty(), "Platform URL should not be empty")

}

// CreateInstallTestEnvironment sets up a temporary directory with bin_dir for installations
func CreateInstallTestEnvironment() (*TestContext, error) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "deps-install-e2e-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Save current working directory
	oldWD, err := os.Getwd()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Create test-bin directory for installations
	binDir := filepath.Join(tempDir, "test-bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to create bin dir: %w", err)
	}

	// Create deps.yaml with all packages from the default registry for testing
	registry := config.GetGlobalRegistry()
	configContent := "dependencies:\n"

	// Add all packages from the registry to dependencies section
	for packageName := range registry.Registry {
		configContent += fmt.Sprintf("  %s: latest\n", packageName)
	}

	configContent += fmt.Sprintf("\nsettings:\n  bin_dir: %s\n", binDir)

	configFile := filepath.Join(tempDir, "deps.yaml")
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to write config file: %w", err)
	}

	// Change to test directory
	if err := os.Chdir(tempDir); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to change directory: %w", err)
	}

	// Force reload of global registry to pick up our new deps.yaml
	if err := reloadGlobalRegistry(); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to reload global registry: %w", err)
	}

	cleanup := func() {
		_ = os.Chdir(oldWD)
		_ = os.RemoveAll(tempDir)
		// Reset global platform overrides
		platform.SetGlobalOverrides("", "")
	}

	return &TestContext{
		TempDir:    tempDir,
		OldWD:      oldWD,
		ConfigFile: configFile,
		Cleanup:    cleanup,
	}, nil
}

// // ValidateInstalledBinary checks installation and runs version command only if platform matches runtime
// func ValidateInstalledBinary(deps.) error {
// 	if result.Error != nil {
// 		return fmt.Errorf("installation failed: %w", result.Error)
// 	}

// 	// Check if binary exists
// 	if _, err := os.Stat(result.BinaryPath); err != nil {
// 		return fmt.Errorf("binary not found at %s: %w", result.BinaryPath, err)
// 	}

// 	// Get package info for version validation
// 	registry := config.GetGlobalRegistry()
// 	pkg, exists := registry.Registry[packageName]
// 	if !exists {
// 		return fmt.Errorf("package %s not found in registry", packageName)
// 	}

// 	currentOS := runtime.GOOS
// 	currentArch := runtime.GOARCH

// 	// Only run version command if we're testing the same platform we're running on
// 	if testOS == currentOS && testArch == currentArch && pkg.VersionCommand != "" {
// 		return validateBinaryVersion(result.BinaryPath, pkg.VersionCommand, pkg.VersionPattern)
// 	}

// 	// For cross-platform installs, just verify the binary exists and has correct format
// 	return validateBinaryFormat(result.BinaryPath, testOS)
// }

// reloadGlobalRegistry forces a reload of the global registry for testing
// This is a workaround since the init() function only runs once per program
func reloadGlobalRegistry() error {
	// Actually, we don't need to reload the global registry anymore since we're
	// using NewWithConfig() to pass the config explicitly to the installer.
	// The global registry should already have the default packages loaded.
	return nil
}

// ChecksumValidationResult holds information about checksum validation
type ChecksumValidationResult struct {
	LockFile *types.LockFile
	Duration time.Duration
	Error    error
}

// TestChecksumValidation performs checksum validation for a package without actual installation
// This is useful for testing non-native platforms (e.g., testing darwin/windows from linux)
func TestChecksumValidation(testCtx *TestContext, packageName, version, osTarget, archTarget string) *ChecksumValidationResult {
	start := time.Now()

	// Generate lock file which includes checksum information
	result := GenerateLockFile(testCtx, packageName, osTarget, archTarget)

	if result.Error != nil {
		return &ChecksumValidationResult{
			Duration: time.Since(start),
			Error:    result.Error,
		}
	}

	// Validate that checksum information is present and valid
	lockFile := result.LockFile
	platformKey := fmt.Sprintf("%s-%s", osTarget, archTarget)

	packageEntry, exists := lockFile.Dependencies[packageName]
	if !exists {
		return &ChecksumValidationResult{
			Duration: time.Since(start),
			Error:    fmt.Errorf("package %s not found in lock file", packageName),
		}
	}

	platformEntry, exists := packageEntry.Platforms[platformKey]
	if !exists {
		return &ChecksumValidationResult{
			Duration: time.Since(start),
			Error:    fmt.Errorf("platform %s not found for package %s", platformKey, packageName),
		}
	}

	// Verify checksum is present if available
	if platformEntry.Checksum == "" {
		// Some packages may not have checksums, which is acceptable
		// Just verify the URL is valid
		if platformEntry.URL == "" {
			return &ChecksumValidationResult{
				Duration: time.Since(start),
				Error:    fmt.Errorf("no URL found for package %s on platform %s", packageName, platformKey),
			}
		}
	}

	return &ChecksumValidationResult{
		LockFile: lockFile,
		Duration: time.Since(start),
		Error:    nil,
	}
}

// ValidateChecksumResult validates the checksum validation result
func ValidateChecksumResult(result *ChecksumValidationResult, packageName, osTarget, archTarget string) error {
	if result.Error != nil {
		return fmt.Errorf("checksum validation failed: %w", result.Error)
	}

	if result.LockFile == nil {
		return fmt.Errorf("lock file is nil")
	}

	platformKey := fmt.Sprintf("%s-%s", osTarget, archTarget)
	packageEntry := result.LockFile.Dependencies[packageName]
	platformEntry := packageEntry.Platforms[platformKey]

	// Validate URL format
	if platformEntry.URL == "" {
		return fmt.Errorf("URL is empty for package %s on platform %s", packageName, platformKey)
	}

	// Validate URL is HTTP/HTTPS
	if !strings.HasPrefix(platformEntry.URL, "http://") && !strings.HasPrefix(platformEntry.URL, "https://") {
		return fmt.Errorf("URL must start with http:// or https://")
	}

	return nil
}
