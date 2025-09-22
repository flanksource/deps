package deps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/commons/files"
	"github.com/flanksource/deps/pkg/version"
)

func TestInstallDependency(t *testing.T) {
	dir, err := os.MkdirTemp("", "deps-test-install")
	if err != nil {
		t.Fatalf("failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(dir)

	// Test a subset of dependencies for faster tests
	testDeps := []string{"jq", "yq", "kubectl", "helm"}

	for _, name := range testDeps {
		t.Run(name, func(t *testing.T) {
			dependency := Dependencies[name]
			t.Logf("Installing %s", name)

			err := InstallDependency(name, dependency.Version, dir)
			if err != nil {
				t.Errorf("Failed to install %s: %v", name, err)
				return
			}

			if len(dependency.PreInstalled) > 0 || dependency.Docker != "" {
				return
			}

			path, err := dependency.GetPath(name, dir)
			if err != nil {
				t.Errorf("Failed to get path for %s: %v", name, err)
			}
			if !files.Exists(path) {
				t.Errorf("Failed to install %s. %s does not exist", name, path)
			}
		})
	}
}

func TestInstallWithOptions(t *testing.T) {
	dir, err := os.MkdirTemp("", "deps-test-options")
	if err != nil {
		t.Fatalf("failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(dir)

	t.Run("basic install with bindir option", func(t *testing.T) {
		var installErr error
		task.StartTask("test-install-jq", func(ctx flanksourceContext.Context, task *task.Task) (interface{}, error) {
			installErr = Install("jq", "1.6", task, WithBinDir(dir))
			return nil, installErr
		})
		err := installErr
		if err != nil {
			t.Errorf("Failed to install jq: %v", err)
		}

		binPath := filepath.Join(dir, "jq")
		if !files.Exists(binPath) {
			t.Errorf("Binary jq was not installed at %s", binPath)
		}
	})

	t.Run("install with force option", func(t *testing.T) {
		// First install
		var installErr error
		task.StartTask("test-install-jq-first", func(ctx flanksourceContext.Context, task *task.Task) (interface{}, error) {
			installErr = Install("jq", "1.6", task, WithBinDir(dir))
			return nil, installErr
		})
		err := installErr
		if err != nil {
			t.Errorf("Failed initial install: %v", err)
		}

		// Force reinstall
		task.StartTask("test-install-jq-force", func(ctx flanksourceContext.Context, task *task.Task) (interface{}, error) {
			installErr = Install("jq", "1.6", task, WithBinDir(dir), WithForce(true))
			return nil, installErr
		})
		err = installErr
		if err != nil {
			t.Errorf("Failed force reinstall: %v", err)
		}
	})

	t.Run("install with timeout", func(t *testing.T) {
		subdir := filepath.Join(dir, "timeout")
		var installErr error
		task.StartTask("test-install-jq-timeout", func(ctx flanksourceContext.Context, task *task.Task) (interface{}, error) {
			installErr = Install("jq", "1.6", task,
				WithBinDir(subdir),
				WithTimeout(30*time.Second))
			return nil, installErr
		})
		err := installErr
		if err != nil {
			t.Errorf("Failed to install with timeout: %v", err)
		}
	})
}

func TestVersionExtraction(t *testing.T) {
	testCases := []struct {
		output    string
		pattern   string
		expected  string
		shouldErr bool
	}{
		// Default pattern
		{"version 1.2.3", "", "1.2.3", false},
		{"v1.2.3-beta", "", "1.2.3-beta", false},
		{"some text v2.0.0 more text", "", "2.0.0", false},

		// Custom patterns
		{"PostgREST 12.0.0", `PostgREST\s+v?(\d+\.\d+\.\d+)`, "12.0.0", false},
		{"wal-g version v3.0.5", `wal-g\s+version\s+v?(\d+\.\d+\.\d+)`, "3.0.5", false},
		{"yq (https://github.com/mikefarah/yq/) version 4.16.2", `yq\s+.*version\s+v?(\d+\.\d+\.\d+)`, "4.16.2", false},

		// Should fail cases
		{"no version here", `version\s+(\d+\.\d+\.\d+)`, "", true},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("extract_%s", strings.ReplaceAll(tc.expected, ".", "_")), func(t *testing.T) {
			result, err := version.ExtractFromOutput(tc.output, tc.pattern)

			if tc.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tc.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tc.shouldErr && result != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	testCases := []struct {
		installed string
		required  string
		mode      VersionCheckMode
		expected  bool
		shouldErr bool
	}{
		// Test exact matching
		{"1.0.0", "1.0.0", VersionCheckExact, true, false},
		{"1.0.1", "1.0.0", VersionCheckExact, false, false},

		// Test minimum version
		{"1.1.0", "1.0.0", VersionCheckMinimum, true, false},
		{"0.9.0", "1.0.0", VersionCheckMinimum, false, false},
		{"1.0.0", "1.0.0", VersionCheckMinimum, true, false},

		// Test compatible version (same major)
		{"1.1.0", "1.0.0", VersionCheckCompatible, true, false},
		{"2.0.0", "1.0.0", VersionCheckCompatible, false, false},

		// Test none (always passes)
		{"anything", "anything", VersionCheckNone, true, false},

		// Test error case with invalid version
		{"not-a-version", "1.0.0", VersionCheckMinimum, false, true},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s_%s_%d", tc.installed, tc.required, tc.mode), func(t *testing.T) {
			result, err := CompareVersions(tc.installed, tc.required, tc.mode)

			if tc.shouldErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tc.shouldErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if !tc.shouldErr && result != tc.expected {
				t.Errorf("Expected %v, got %v for installed=%s, required=%s, mode=%d",
					tc.expected, result, tc.installed, tc.required, tc.mode)
			}
		})
	}
}

func TestInstallPostgres(t *testing.T) {
	// Skip this test in CI as it downloads large files
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping Postgres installation test in CI")
	}

	dir, err := os.MkdirTemp("", "deps-test-postgres")
	if err != nil {
		t.Fatalf("failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(dir)

	var installErr error
	task.StartTask("test-install-postgres", func(ctx flanksourceContext.Context, task *task.Task) (interface{}, error) {
		installErr = Install("postgres", "16.1.0", task, WithBinDir(dir))
		return nil, installErr
	})
	err = installErr
	if err != nil {
		t.Errorf("Failed to install Postgres: %v", err)
	}

	// Check if postgres binaries were extracted
	postgresDir := filepath.Join(dir, "postgres")
	if !files.Exists(postgresDir) {
		t.Errorf("Postgres directory was not created at %s", postgresDir)
	}
}

func TestBackwardCompatibility(t *testing.T) {
	dir, err := os.MkdirTemp("", "deps-test-compat")
	if err != nil {
		t.Fatalf("failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(dir)

	// The old InstallDependency function should still work
	err = InstallDependency("jq", "1.6", dir)
	if err != nil {
		t.Errorf("InstallDependency (legacy function) failed: %v", err)
	}

	binPath := filepath.Join(dir, "jq")
	if !files.Exists(binPath) {
		t.Errorf("Binary jq was not installed using legacy InstallDependency function")
	}
}

func TestOptionFunctions(t *testing.T) {
	opts := &InstallOptions{}

	// Test WithBinDir
	WithBinDir("/test/path")(opts)
	if opts.BinDir != "/test/path" {
		t.Errorf("WithBinDir failed: expected /test/path, got %s", opts.BinDir)
	}

	// Test WithForce
	WithForce(true)(opts)
	if !opts.Force {
		t.Errorf("WithForce failed: expected true, got %v", opts.Force)
	}

	// Test WithVersionCheck
	WithVersionCheck(VersionCheckExact)(opts)
	if opts.VersionCheck != VersionCheckExact {
		t.Errorf("WithVersionCheck failed: expected %d, got %d", VersionCheckExact, opts.VersionCheck)
	}

	// Test WithOS
	WithOS("linux", "arm64")(opts)
	if opts.GOOS != "linux" || opts.GOARCH != "arm64" {
		t.Errorf("WithOS failed: expected linux/arm64, got %s/%s", opts.GOOS, opts.GOARCH)
	}

	// Test WithTimeout
	timeout := 10 * time.Minute
	WithTimeout(timeout)(opts)
	if opts.Timeout != timeout {
		t.Errorf("WithTimeout failed: expected %v, got %v", timeout, opts.Timeout)
	}

	// Test WithSkipChecksum
	WithSkipChecksum(true)(opts)
	if !opts.SkipChecksum {
		t.Errorf("WithSkipChecksum failed: expected true, got %v", opts.SkipChecksum)
	}

	// Test WithPreferLocal
	WithPreferLocal(true)(opts)
	if !opts.PreferLocal {
		t.Errorf("WithPreferLocal failed: expected true, got %v", opts.PreferLocal)
	}
}
