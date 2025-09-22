//go:build integration

package deps

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/flanksource/deps/mock"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/lock"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
)

// Test helpers

// createTestDir creates a temporary directory with test configuration files
func createTestDir(t *testing.T, configName string) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "deps-integration-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	// Get the directory where this source file is located
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("could not determine source file location")
	}

	// The testdata directory should be in the same directory as this integration_test.go file
	sourceDir := filepath.Dir(filename)
	srcPath := filepath.Join(sourceDir, "testdata", configName)

	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("could not find testdata file %s at %s: %v", configName, srcPath, err)
	}

	dstPath := filepath.Join(dir, "deps.yaml")

	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("failed to read test config %s: %v", srcPath, err)
	}

	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	return dir
}

// createTestDirWithLock creates a temporary directory with both deps.yaml and deps-lock.yaml
func createTestDirWithLock(t *testing.T) string {
	t.Helper()

	dir := createTestDir(t, "minimal-deps.yaml")

	// Get the directory where this source file is located
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("could not determine source file location")
	}

	sourceDir := filepath.Dir(filename)
	srcPath := filepath.Join(sourceDir, "testdata", "deps-lock.yaml")

	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("could not find testdata file deps-lock.yaml at %s: %v", srcPath, err)
	}

	dstPath := filepath.Join(dir, "deps-lock.yaml")

	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("failed to read test lock file: %v", err)
	}

	if err := os.WriteFile(dstPath, data, 0644); err != nil {
		t.Fatalf("failed to write test lock file: %v", err)
	}

	return dir
}

// setupMockRegistry creates a registry with mock package managers for testing
func setupMockRegistry() *manager.Registry {
	registry := manager.NewRegistry()

	// Mock GitHub release manager
	githubMock := mock.NewMockPackageManager("github_release").
		WithVersions("1.8.1", "1.7.0", "1.6.0").
		WithChecksum("jq-darwin-amd64", "sha256:mock-jq-checksum")
	registry.Register(githubMock)

	// Mock direct URL manager
	directMock := mock.NewMockPackageManager("direct").
		WithVersions("1.28.2", "1.28.1", "1.28.0").
		WithChecksum("kubectl-darwin-amd64", "sha256:mock-kubectl-checksum")
	registry.Register(directMock)

	return registry
}

// Integration Tests

func TestVersionNormalizationIntegration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		installedVersion string
		expectedVersion  string
		shouldMatch      bool
		description      string
	}{
		{
			name:             "v-prefix-match",
			installedVersion: "v3.5.0",
			expectedVersion:  "3.5.0",
			shouldMatch:      true,
			description:      "v-prefixed version should match non-prefixed",
		},
		{
			name:             "reverse-v-prefix-match",
			installedVersion: "3.5.0",
			expectedVersion:  "v3.5.0",
			shouldMatch:      true,
			description:      "non-prefixed version should match v-prefixed",
		},
		{
			name:             "release-prefix-match",
			installedVersion: "release-1.2.3",
			expectedVersion:  "1.2.3",
			shouldMatch:      true,
			description:      "release-prefixed version should match plain version",
		},
		{
			name:             "version-mismatch",
			installedVersion: "v3.4.0",
			expectedVersion:  "3.5.0",
			shouldMatch:      false,
			description:      "different versions should not match",
		},
	}

	for _, tc := range testCases {
		tc := tc // capture for parallel execution
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Test version comparison
			cmp, err := version.Compare(tc.installedVersion, tc.expectedVersion)
			if err != nil {
				t.Fatalf("version comparison failed: %v", err)
			}

			isEqual := (cmp == 0)
			if isEqual != tc.shouldMatch {
				t.Errorf("%s: expected match=%t, got match=%t (compare result: %d)",
					tc.description, tc.shouldMatch, isEqual, cmp)
			}

			// Also test with normalization
			normalized1 := version.Normalize(tc.installedVersion)
			normalized2 := version.Normalize(tc.expectedVersion)

			cmp2, err := version.Compare(normalized1, normalized2)
			if err != nil {
				t.Fatalf("normalized version comparison failed: %v", err)
			}

			isEqualNormalized := (cmp2 == 0)
			if isEqualNormalized != tc.shouldMatch {
				t.Errorf("%s: normalized comparison expected match=%t, got match=%t (normalized: %s vs %s)",
					tc.description, tc.shouldMatch, isEqualNormalized, normalized1, normalized2)
			}
		})
	}
}

func TestInstallAllFromConfig(t *testing.T) {
	// NOTE: Not using t.Parallel() because this test changes working directory

	// Create test environment - use minimal-deps.yaml for simpler testing
	dir := createTestDir(t, "minimal-deps.yaml")
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to test directory: %v", err)
	}

	// Load configuration to verify expected dependencies
	depsConfig, err := config.LoadDepsConfig("")
	if err != nil {
		t.Fatalf("failed to load deps config: %v", err)
	}

	expectedDeps := []string{"jq"} // minimal-deps.yaml only has jq
	if len(depsConfig.Dependencies) != len(expectedDeps) {
		t.Fatalf("expected %d dependencies, got %d", len(expectedDeps), len(depsConfig.Dependencies))
	}

	// Verify all expected dependencies are present
	for _, dep := range expectedDeps {
		if _, exists := depsConfig.Dependencies[dep]; !exists {
			t.Errorf("expected dependency %s not found in config", dep)
		}
	}

	// Test registry lookup
	for depName := range depsConfig.Dependencies {
		if _, exists := depsConfig.Registry[depName]; !exists {
			t.Errorf("dependency %s not found in registry", depName)
		}
	}

	t.Logf("Successfully validated configuration with %d dependencies", len(depsConfig.Dependencies))
}

func TestLockFileConstraintResolution(t *testing.T) {
	// NOTE: Not using t.Parallel() because this test changes working directory

	ctx := context.Background()
	dir := createTestDir(t, "minimal-deps.yaml")

	// Load the test configuration
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to test directory: %v", err)
	}

	depsConfig, err := config.LoadDepsConfig("")
	if err != nil {
		t.Fatalf("failed to load deps config: %v", err)
	}

	// Set up mock registry
	registry := setupMockRegistry()
	generator := lock.NewGenerator(registry)

	// Test constraint resolution (use simple constraints that our mock can handle)
	// The mock will just return the first version in its list
	constraints := map[string]string{
		"jq": "latest", // Mock returns 1.8.1 as latest
	}

	// Generate lock file
	lockOpts := types.LockOptions{
		Platforms: []string{"darwin-amd64", "linux-amd64"},
	}

	lockFile, err := generator.Generate(ctx, constraints, depsConfig.Registry, lockOpts)
	if err != nil {
		t.Fatalf("failed to generate lock file: %v", err)
	}

	// Verify lock file structure
	if lockFile.Version != "1.0" {
		t.Errorf("expected lock file version 1.0, got %s", lockFile.Version)
	}

	if len(lockFile.Dependencies) != len(constraints) {
		t.Errorf("expected %d locked dependencies, got %d",
			len(constraints), len(lockFile.Dependencies))
	}

	// Verify specific constraint resolution
	for depName, constraint := range constraints {
		lockEntry, exists := lockFile.Dependencies[depName]
		if !exists {
			t.Errorf("dependency %s not found in lock file", depName)
			continue
		}

		t.Logf("Dependency %s: constraint %s resolved to %s",
			depName, constraint, lockEntry.Version)

		if lockEntry.Version == "" {
			t.Errorf("dependency %s has empty version", depName)
		}

		// Verify platforms
		if len(lockEntry.Platforms) == 0 {
			t.Errorf("dependency %s has no platforms", depName)
		}

		// Verify version commands are preserved
		if lockEntry.VersionCommand == "" {
			t.Errorf("dependency %s missing version command", depName)
		}
	}
}

func TestUpdateCheckFlow(t *testing.T) {
	// NOTE: Not using t.Parallel() because this test changes working directory

	// Create test environment with lock file (simulating installed dependencies)
	dir := createTestDirWithLock(t)
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to test directory: %v", err)
	}

	// Load configurations
	depsConfig, err := config.LoadDepsConfig("")
	if err != nil {
		t.Fatalf("failed to load deps config: %v", err)
	}

	lockFile, err := config.LoadLockFile("")
	if err != nil {
		t.Fatalf("failed to load lock file: %v", err)
	}

	// Set up mock registry with newer versions available
	registry := setupMockRegistry()

	// Simulate update check logic
	ctx := context.Background()
	updates := []struct {
		name             string
		currentVersion   string
		availableVersion string
		needsUpdate      bool
	}{}

	for depName := range depsConfig.Dependencies {
		lockEntry, hasLock := lockFile.Dependencies[depName]
		pkg, hasPkg := depsConfig.Registry[depName]

		if !hasPkg {
			continue
		}

		// Get package manager
		mgr, err := registry.GetForPackage(pkg)
		if err != nil {
			t.Logf("Warning: no package manager for %s: %v", depName, err)
			continue
		}

		// Get available versions
		versions, err := mgr.DiscoverVersions(ctx, pkg, "")
		if err != nil || len(versions) == 0 {
			continue
		}

		current := ""
		if hasLock {
			current = lockEntry.Version
		}

		available := versions[0].Version // Latest version

		// Check if update needed
		needsUpdate := false
		if current != "" && available != "" {
			cmp, err := version.Compare(available, current)
			if err == nil && cmp > 0 {
				needsUpdate = true
			}
		}

		updates = append(updates, struct {
			name             string
			currentVersion   string
			availableVersion string
			needsUpdate      bool
		}{
			name:             depName,
			currentVersion:   current,
			availableVersion: available,
			needsUpdate:      needsUpdate,
		})
	}

	// Verify update detection worked
	if len(updates) == 0 {
		t.Fatal("no updates were processed")
	}

	// Log update information
	for _, update := range updates {
		t.Logf("Update check: %s current=%s available=%s needs_update=%t",
			update.name, update.currentVersion, update.availableVersion, update.needsUpdate)
	}
}

func TestCLICommands(t *testing.T) {
	// NOTE: Not using t.Parallel() because this test changes working directory

	// Create test environment
	dir, err := os.MkdirTemp("", "deps-cli-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer os.Chdir(oldWd)

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change to test directory: %v", err)
	}

	t.Run("init_command_creates_config", func(t *testing.T) {
		// Test that init creates a valid configuration
		defaultConfig := config.CreateDefaultConfig()

		// Add test dependencies (use simple constraints for testing)
		defaultConfig.Dependencies = map[string]string{
			"jq": "latest",
		}
		defaultConfig.Registry = map[string]types.Package{
			"jq": {
				Name:    "jq",
				Manager: "github_release",
				Repo:    "jqlang/jq",
				AssetPatterns: map[string]string{
					"linux-amd64":  "jq-linux64",
					"darwin-amd64": "jq-osx-amd64",
				},
				VersionCommand: "--version",
				VersionPattern: `jq-(\d+\.\d+)`,
			},
		}

		// Save the configuration
		if err := config.SaveDepsConfig(defaultConfig, ""); err != nil {
			t.Fatalf("failed to save config: %v", err)
		}

		// Verify the file was created and is valid
		if _, err := os.Stat("deps.yaml"); err != nil {
			t.Fatalf("deps.yaml was not created: %v", err)
		}

		// Load and verify
		loaded, err := config.LoadDepsConfig("")
		if err != nil {
			t.Fatalf("failed to load created config: %v", err)
		}

		if len(loaded.Dependencies) == 0 {
			t.Error("created config has no dependencies")
		}

		if len(loaded.Registry) == 0 {
			t.Error("created config has no registry entries")
		}
	})

	t.Run("lock_generation", func(t *testing.T) {
		// Load the config created in the previous test
		depsConfig, err := config.LoadDepsConfig("")
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}

		// Create mock lock generator
		registry := setupMockRegistry()
		generator := lock.NewGenerator(registry)

		// Generate lock file with multiple platforms
		ctx := context.Background()
		lockOpts := types.LockOptions{
			Platforms: []string{"linux-amd64", "darwin-amd64"},
			Parallel:  false, // Use sequential for test predictability
		}

		lockFile, err := generator.Generate(ctx, depsConfig.Dependencies, depsConfig.Registry, lockOpts)
		if err != nil {
			t.Fatalf("failed to generate lock file: %v", err)
		}

		// Verify lock file properties
		if lockFile.Version != "1.0" {
			t.Errorf("expected version 1.0, got %s", lockFile.Version)
		}

		if lockFile.CurrentPlatform.OS == "" {
			t.Error("current platform OS not set")
		}

		if time.Since(lockFile.Generated) > time.Minute {
			t.Error("generated timestamp seems too old")
		}

		// Verify dependencies are locked
		for depName := range depsConfig.Dependencies {
			lockEntry, exists := lockFile.Dependencies[depName]
			if !exists {
				t.Errorf("dependency %s not found in lock file", depName)
				continue
			}

			if lockEntry.Version == "" {
				t.Errorf("dependency %s has empty version in lock file", depName)
			}

			// Check that we have platform entries
			if len(lockEntry.Platforms) == 0 {
				t.Errorf("dependency %s has no platform entries", depName)
			}

			// Verify platform entries have required fields
			for platformStr, platformEntry := range lockEntry.Platforms {
				if platformEntry.URL == "" {
					t.Errorf("dependency %s platform %s missing URL", depName, platformStr)
				}
			}
		}

		// Save and verify file can be loaded
		if err := config.SaveLockFile(lockFile, ""); err != nil {
			t.Fatalf("failed to save lock file: %v", err)
		}

		// Verify lock file was created
		if _, err := os.Stat("deps-lock.yaml"); err != nil {
			t.Fatalf("deps-lock.yaml was not created: %v", err)
		}

		// Load and verify
		reloaded, err := config.LoadLockFile("")
		if err != nil {
			t.Fatalf("failed to reload lock file: %v", err)
		}

		if len(reloaded.Dependencies) != len(lockFile.Dependencies) {
			t.Errorf("reloaded lock file has different dependency count: expected %d, got %d",
				len(lockFile.Dependencies), len(reloaded.Dependencies))
		}
	})
}

func TestErrorHandling(t *testing.T) {
	// NOTE: Not using t.Parallel() because this test changes working directory

	t.Run("missing_deps_file", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "deps-error-test")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })

		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get working directory: %v", err)
		}
		defer os.Chdir(oldWd)

		if err := os.Chdir(dir); err != nil {
			t.Fatalf("failed to change to test directory: %v", err)
		}

		// Try to load non-existent config
		_, err = config.LoadDepsConfig("")
		if err == nil {
			t.Error("expected error when loading non-existent config file")
		}

		if !strings.Contains(err.Error(), "failed to read deps config file") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("invalid_yaml", func(t *testing.T) {
		dir, err := os.MkdirTemp("", "deps-error-test")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })

		// Create invalid YAML file
		invalidYaml := `dependencies:
  jq: latest
  invalid yaml structure [[[`

		depsFile := filepath.Join(dir, "deps.yaml")
		if err := os.WriteFile(depsFile, []byte(invalidYaml), 0644); err != nil {
			t.Fatalf("failed to write invalid yaml: %v", err)
		}

		oldWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get working directory: %v", err)
		}
		defer os.Chdir(oldWd)

		if err := os.Chdir(dir); err != nil {
			t.Fatalf("failed to change to test directory: %v", err)
		}

		// Try to load invalid config
		_, err = config.LoadDepsConfig("")
		if err == nil {
			t.Error("expected error when loading invalid YAML")
			return
		}

		if !strings.Contains(err.Error(), "failed to parse deps config file") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
