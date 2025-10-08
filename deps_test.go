package deps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/types"
)

func TestInstall(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "deps-api-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("Failed to create bin dir: %v", err)
	}

	// Set up a minimal test config
	testConfig := &types.DepsConfig{
		Dependencies: map[string]string{
			"yq": "latest",
		},
		Registry: map[string]types.Package{
			"yq": {
				Name:    "yq",
				Manager: "github_release",
				Repo:    "mikefarah/yq",
				AssetPatterns: map[string]string{
					"*": "*{{.os}}_{{.arch}}*",
				},
				VersionCommand: "yq --version",
			},
		},
		Settings: types.Settings{
			BinDir: binDir,
		},
	}

	// Register test config globally
	config.SetGlobalRegistry(testConfig)

	t.Run("InstallReturnsResult", func(t *testing.T) {
		result, err := Install("yq", "v4.35.1",
			WithBinDir(binDir),
			WithSkipChecksum(true))

		if err != nil {
			t.Fatalf("Install failed: %v", err)
		}

		if result == nil {
			t.Fatal("Result should not be nil")
		}

		if result.Package.Name != "yq" {
			t.Errorf("Expected package name 'yq', got '%s'", result.Package.Name)
		}

		if result.Version.Version != "v4.35.1" {
			t.Errorf("Expected version 'v4.35.1', got '%s'", result.Version.Version)
		}

		if result.Status == "" {
			t.Error("Status should not be empty")
		}

		if result.BinDir != binDir {
			t.Errorf("Expected bin dir '%s', got '%s'", binDir, result.BinDir)
		}

		if result.Duration == 0 {
			t.Error("Duration should be greater than 0")
		}
	})

	t.Run("PrettyOutput", func(t *testing.T) {
		result, err := Install("yq", "v4.35.1",
			WithBinDir(binDir),
			WithForce(true),
			WithSkipChecksum(true))

		if err != nil {
			t.Fatalf("Install failed: %v", err)
		}

		pretty := result.Pretty()
		prettyStr := pretty.String()

		if prettyStr == "" {
			t.Error("Pretty output should not be empty")
		}

		t.Logf("Pretty output:\n%s", prettyStr)
	})
}

func TestInstallResult_Pretty(t *testing.T) {
	tests := []struct {
		name   string
		result InstallResult
	}{
		{
			name: "Successful installation",
			result: InstallResult{
				Package: types.Package{Name: "test-package"},
				Version: types.Version{Version: "1.0.0"},
				Status:  InstallStatusInstalled,
				BinDir:  "/usr/local/bin",
			},
		},
		{
			name: "Failed installation",
			result: InstallResult{
				Package: types.Package{Name: "test-package"},
				Status:  InstallStatusFailed,
				Error:   os.ErrNotExist,
			},
		},
		{
			name: "Already installed",
			result: InstallResult{
				Package: types.Package{Name: "test-package"},
				Version: types.Version{Version: "1.0.0"},
				Status:  InstallStatusAlreadyInstalled,
				BinDir:  "/usr/local/bin",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pretty := tt.result.Pretty()
			if pretty.String() == "" {
				t.Error("Pretty output should not be empty")
			}
			t.Logf("Pretty output for %s:\n%s", tt.name, pretty.String())
		})
	}
}
