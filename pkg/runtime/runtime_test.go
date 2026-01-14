package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/flanksource/deps"
)

func init() {
	// Add common runtime installation directories to PATH for tests
	homeDir, err := os.UserHomeDir()
	if err == nil {
		localOptDir := filepath.Join(homeDir, ".local", "opt")
		if entries, err := os.ReadDir(localOptDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					binPath := filepath.Join(localOptDir, entry.Name())
					_ = os.Setenv("PATH", binPath+string(os.PathListSeparator)+os.Getenv("PATH"))
				}
			}
		}
	}

	// Add Homebrew opt directories (macOS)
	if entries, err := os.ReadDir("/usr/local/opt"); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				binPath := filepath.Join("/usr/local/opt", entry.Name(), "bin")
				if _, err := os.Stat(binPath); err == nil {
					_ = os.Setenv("PATH", binPath+string(os.PathListSeparator)+os.Getenv("PATH"))
				}
			}
		}
	}
}

func TestRunPython(t *testing.T) {
	t.Run("execute simple Python script", func(t *testing.T) {
		result, err := deps.RunPython("testdata/test_python_runtime.py", deps.RunOptions{
			Timeout: 10 * time.Second,
			Env: map[string]string{
				"TEST_API_KEY": "test_value_123",
			},
		})

		if err != nil {
			t.Fatalf("RunPython failed: %v", err)
		}

		// Check output
		stdout := result.GetStdout()
		if !strings.Contains(stdout, "Python Runtime Test") {
			t.Errorf("Expected output to contain 'Python Runtime Test', got: %s", stdout)
		}

		if !strings.Contains(stdout, "TEST_API_KEY: test_value_123") {
			t.Errorf("Expected environment variable to be set, got: %s", stdout)
		}

		if !strings.Contains(stdout, "Test completed successfully!") {
			t.Errorf("Expected output to contain 'Test completed successfully!', got: %s", stdout)
		}

		// Check runtime metadata
		if result.RuntimePath == "" {
			t.Error("RuntimePath should not be empty")
		}

		if result.RuntimeVersion == "" {
			t.Error("RuntimeVersion should not be empty")
		}

		t.Logf("Python runtime: %s version %s", result.RuntimePath, result.RuntimeVersion)
		t.Logf("Output:\n%s", stdout)
	})
}

func TestRunNode(t *testing.T) {
	t.Run("execute simple Node script", func(t *testing.T) {
		result, err := deps.RunNode("testdata/test_node_runtime.js", deps.RunOptions{
			Timeout: 10 * time.Second,
			Env: map[string]string{
				"TEST_API_KEY": "test_value_456",
			},
		})

		if err != nil {
			t.Fatalf("RunNode failed: %v", err)
		}

		// Check output
		stdout := result.Out()
		if !strings.Contains(stdout, "Node.js Runtime Test") {
			t.Errorf("Expected output to contain 'Node.js Runtime Test', got: %s", stdout)
		}

		if !strings.Contains(stdout, "TEST_API_KEY: test_value_456") {
			t.Errorf("Expected environment variable to be set, got: %s", stdout)
		}

		if !strings.Contains(stdout, "Test completed successfully!") {
			t.Errorf("Expected output to contain 'Test completed successfully!', got: %s", stdout)
		}

		// Check runtime metadata
		if result.RuntimePath == "" {
			t.Error("RuntimePath should not be empty")
		}

		if result.RuntimeVersion == "" {
			t.Error("RuntimeVersion should not be empty")
		}

		t.Logf("Node runtime: %s version %s", result.RuntimePath, result.RuntimeVersion)
		t.Logf("Output:\n%s", stdout)
	})
}

func TestRunJava(t *testing.T) {
	t.Run("execute simple Java source file", func(t *testing.T) {
		result, err := deps.RunJava("testdata/TestJavaRuntime.java", deps.RunOptions{
			Timeout: 30 * time.Second,
			Env: map[string]string{
				"TEST_API_KEY": "test_value_789",
			},
		})

		if err != nil {
			stderr := ""
			stdout := ""
			if result != nil {
				stderr = result.GetStderr()
				stdout = result.GetOutput()
			}
			t.Fatalf("RunJava failed: %v\nStdout: %s\nStderr: %s", err, stdout, stderr)
		}

		// Check output
		stdout := result.GetStdout()
		if !strings.Contains(stdout, "Java Runtime Test") {
			t.Errorf("Expected output to contain 'Java Runtime Test', got: %s", stdout)
		}

		if !strings.Contains(stdout, "TEST_API_KEY: test_value_789") {
			t.Errorf("Expected environment variable to be set, got: %s", stdout)
		}

		if !strings.Contains(stdout, "Test completed successfully!") {
			t.Errorf("Expected output to contain 'Test completed successfully!', got: %s", stdout)
		}

		// Check runtime metadata
		if result.RuntimePath == "" {
			t.Error("RuntimePath should not be empty")
		}

		if result.RuntimeVersion == "" {
			t.Error("RuntimeVersion should not be empty")
		}

		t.Logf("Java runtime: %s version %s", result.RuntimePath, result.RuntimeVersion)
		t.Logf("Output:\n%s", stdout)
	})
}

func TestRunPowershell(t *testing.T) {
	t.Run("execute simple PowerShell script", func(t *testing.T) {
		result, err := deps.RunPowershell("testdata/test_powershell_runtime.ps1", deps.RunOptions{
			Timeout: 10 * time.Second,
			Version: "7.4.6",
			Env: map[string]string{
				"TEST_API_KEY": "test_value_abc",
			},
		})

		if err != nil {
			stderr := ""
			stdout := ""
			if result != nil {
				stderr = result.GetStderr()
				stdout = result.GetStdout()
			}
			t.Fatalf("RunPowershell failed: %v\nStdout: %s\nStderr: %s", err, stdout, stderr)
		}

		// Check output
		stdout := result.GetStdout()
		if !strings.Contains(stdout, "PowerShell Runtime Test") {
			t.Errorf("Expected output to contain 'PowerShell Runtime Test', got: %s", stdout)
		}

		if !strings.Contains(stdout, "TEST_API_KEY: test_value_abc") {
			t.Errorf("Expected environment variable to be set, got: %s", stdout)
		}

		if !strings.Contains(stdout, "Test completed successfully!") {
			t.Errorf("Expected output to contain 'Test completed successfully!', got: %s", stdout)
		}

		// Check runtime metadata
		if result.RuntimePath == "" {
			t.Error("RuntimePath should not be empty")
		}

		if result.RuntimeVersion == "" {
			t.Error("RuntimeVersion should not be empty")
		}

		t.Logf("PowerShell runtime: %s version %s", result.RuntimePath, result.RuntimeVersion)
		t.Logf("Output:\n%s", stdout)
	})
}
