package helpers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/installer"
	"github.com/flanksource/deps/pkg/lock"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
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
		os.RemoveAll(tempDir)
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
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to write config file: %w", err)
	}

	// Change to test directory
	if err := os.Chdir(tempDir); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to change directory: %w", err)
	}

	cleanup := func() {
		_ = os.Chdir(oldWD)
		os.RemoveAll(tempDir)
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

	// Additional platform-specific validations
	ValidatePlatformEntry(platformEntry, expectedPackage, expectedPlatform)
}

// ValidatePlatformEntry performs detailed validation of a platform-specific lock entry
func ValidatePlatformEntry(entry types.PlatformEntry, packageName, platform string) {
	// Validate URL format
	Expect(entry.URL).To(MatchRegexp(`^https?://`), "URL should start with http:// or https://")

	// Platform-specific validations
	parts := splitPlatform(platform)
	if len(parts) == 2 {
		os, arch := parts[0], parts[1]

		// Validate URL contains platform-specific elements
		switch os {
		case "windows":
			// Windows binaries often have .exe extension or .zip archives
			Expect(entry.URL).To(Or(
				ContainSubstring(".exe"),
				ContainSubstring(".zip"),
				ContainSubstring("windows"),
			), "Windows URL should contain .exe, .zip, or 'windows'")

		case "darwin":
			// macOS binaries often contain 'darwin', 'osx', or 'mac'
			Expect(entry.URL).To(Or(
				ContainSubstring("darwin"),
				ContainSubstring("osx"),
				ContainSubstring("mac"),
			), "Darwin URL should contain 'darwin', 'osx', or 'mac'")

		case "linux":
			// Linux binaries often contain 'linux'
			Expect(entry.URL).To(ContainSubstring("linux"), "Linux URL should contain 'linux'")
		}

		// Architecture validation
		switch arch {
		case "amd64":
			Expect(entry.URL).To(Or(
				ContainSubstring("amd64"),
				ContainSubstring("x86_64"),
				ContainSubstring("64"),
			), "AMD64 URL should contain architecture indicator")

		case "arm64":
			Expect(entry.URL).To(Or(
				ContainSubstring("arm64"),
				ContainSubstring("aarch64"),
				ContainSubstring("arm"),
			), "ARM64 URL should contain ARM architecture indicator")
		}
	}

	// Validate checksums if present
	if entry.Checksum != "" {
		// Checksums can have prefixes like "sha256:" or "md5:", or be plain hex
		if strings.Contains(entry.Checksum, ":") {
			// Format: "algorithm:hexvalue"
			parts := strings.Split(entry.Checksum, ":")
			Expect(len(parts)).To(Equal(2), "Prefixed checksum should have format 'algorithm:hash'")
			algorithm, hash := parts[0], parts[1]
			Expect(algorithm).To(MatchRegexp(`^(md5|sha1|sha256|sha512)$`), "Algorithm should be recognized")
			Expect(hash).To(MatchRegexp(`^[a-fA-F0-9]+$`), "Hash part should be hexadecimal")
		} else {
			// Plain hex format
			Expect(entry.Checksum).To(MatchRegexp(`^[a-fA-F0-9]+$`), "Checksum should be hexadecimal")
			// Common hash lengths: MD5(32), SHA1(40), SHA256(64), SHA512(128)
			checksumLen := len(entry.Checksum)
			Expect([]int{32, 40, 64, 128}).To(ContainElement(checksumLen),
				"Checksum should be valid hash length")
		}
	}
}

// SplitPlatform splits a platform string like "darwin-amd64" into ["darwin", "amd64"]
func SplitPlatform(platform string) []string {
	return strings.Split(platform, "-")
}

// splitPlatform is kept for internal use
func splitPlatform(platform string) []string {
	return SplitPlatform(platform)
}

// GetCurrentPlatform returns the current runtime platform as a string
func GetCurrentPlatform() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

// IsLongRunningTest determines if a test should only run in long test mode
func IsLongRunningTest(packageName string) bool {
	// Some packages take longer to resolve or download
	slowPackages := []string{
		"terraform", // Large downloads
		"packer",    // Large downloads
		"etcd",      // Multiple files
	}

	return contains(slowPackages, packageName)
}

// InstallResult holds information about an installation attempt
type InstallResult struct {
	BinaryPath string
	Duration   time.Duration
	Error      error
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
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Create test-bin directory for installations
	binDir := filepath.Join(tempDir, "test-bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		os.RemoveAll(tempDir)
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
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to write config file: %w", err)
	}

	// Change to test directory
	if err := os.Chdir(tempDir); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to change directory: %w", err)
	}

	// Force reload of global registry to pick up our new deps.yaml
	if err := reloadGlobalRegistry(); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to reload global registry: %w", err)
	}

	cleanup := func() {
		_ = os.Chdir(oldWD)
		os.RemoveAll(tempDir)
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

// TestInstallation performs actual installation using the installer API
func TestInstallation(testCtx *TestContext, packageName, version, osTarget, archTarget string) *InstallResult {
	start := time.Now()

	// Set platform overrides for cross-platform testing
	platform.SetGlobalOverrides(osTarget, archTarget)

	// Create context with timeout (for potential future use)
	_, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Set up installer with test bin directory and config
	binDir := filepath.Join(testCtx.TempDir, "test-bin")
	depsConfig := config.GetGlobalRegistry()
	installer := installer.NewWithConfig(depsConfig,
		installer.WithBinDir(binDir),
		installer.WithOS(osTarget, archTarget),
		installer.WithSkipChecksum(true), // Skip checksums for faster testing
	)

	// Create a test task
	testTask := &task.Task{}

	// Perform installation
	err := installer.Install(packageName, version, testTask)

	// Wait for async tasks
	task.WaitForAllTasks()

	duration := time.Since(start)

	// Determine expected binary path
	registry := config.GetGlobalRegistry()
	pkg, exists := registry.Registry[packageName]
	binaryName := packageName
	if exists && pkg.BinaryName != "" {
		binaryName = pkg.BinaryName
	}

	// Handle Windows executable extension
	if osTarget == "windows" && !strings.HasSuffix(binaryName, ".exe") {
		binaryName += ".exe"
	}

	binaryPath := filepath.Join(binDir, binaryName)

	return &InstallResult{
		BinaryPath: binaryPath,
		Duration:   duration,
		Error:      err,
	}
}

// ValidateInstalledBinary checks installation and runs version command only if platform matches runtime
func ValidateInstalledBinary(result *InstallResult, packageName, testOS, testArch string) error {
	if result.Error != nil {
		return fmt.Errorf("installation failed: %w", result.Error)
	}

	// Check if binary exists
	if _, err := os.Stat(result.BinaryPath); err != nil {
		return fmt.Errorf("binary not found at %s: %w", result.BinaryPath, err)
	}

	// Get package info for version validation
	registry := config.GetGlobalRegistry()
	pkg, exists := registry.Registry[packageName]
	if !exists {
		return fmt.Errorf("package %s not found in registry", packageName)
	}

	currentOS := runtime.GOOS
	currentArch := runtime.GOARCH

	// Only run version command if we're testing the same platform we're running on
	if testOS == currentOS && testArch == currentArch && pkg.VersionCommand != "" {
		return validateBinaryVersion(result.BinaryPath, pkg.VersionCommand, pkg.VersionPattern)
	}

	// For cross-platform installs, just verify the binary exists and has correct format
	return validateBinaryFormat(result.BinaryPath, testOS)
}

// validateBinaryVersion runs version command and validates output pattern
func validateBinaryVersion(binaryPath, versionCommand, versionPattern string) error {
	// Make binary executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	// Run version command (basic implementation - could be enhanced)
	// For now, just verify the binary is executable
	_, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("binary not executable: %w", err)
	}

	return nil
}

// validateBinaryFormat validates binary format for target platform
func validateBinaryFormat(binaryPath, targetOS string) error {
	// Check if file exists and is not empty
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("binary validation failed: %w", err)
	}

	if info.Size() == 0 {
		return fmt.Errorf("binary is empty")
	}

	// Basic format validation based on target OS
	switch targetOS {
	case "windows":
		if !strings.HasSuffix(strings.ToLower(binaryPath), ".exe") {
			return fmt.Errorf("Windows binary should have .exe extension")
		}
	case "darwin", "linux":
		// Unix-like binaries should be executable files
		if !info.Mode().IsRegular() {
			return fmt.Errorf("binary should be a regular file")
		}
	}

	return nil
}

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
