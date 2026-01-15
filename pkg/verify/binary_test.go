package verify

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectBinaryPlatform(t *testing.T) {
	// Create a simple Go binary for testing
	tempDir := t.TempDir()

	// Write a simple Go program
	srcFile := filepath.Join(tempDir, "main.go")
	err := os.WriteFile(srcFile, []byte(`package main; func main() {}`), 0644)
	require.NoError(t, err)

	tests := []struct {
		name         string
		goos         string
		goarch       string
		expectedOS   string
		expectedArch string
		expectedType string
	}{
		{"linux-amd64", "linux", "amd64", "linux", "amd64", "elf"},
		{"linux-arm64", "linux", "arm64", "linux", "arm64", "elf"},
		{"darwin-amd64", "darwin", "amd64", "darwin", "amd64", "macho"},
		{"darwin-arm64", "darwin", "arm64", "darwin", "arm64", "macho"},
		{"windows-amd64", "windows", "amd64", "windows", "amd64", "pe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build binary for target platform
			binaryName := "testbin"
			if tt.goos == "windows" {
				binaryName += ".exe"
			}
			binaryPath := filepath.Join(tempDir, binaryName+"-"+tt.name)

			cmd := exec.Command("go", "build", "-o", binaryPath, srcFile)
			cmd.Env = append(os.Environ(), "GOOS="+tt.goos, "GOARCH="+tt.goarch, "CGO_ENABLED=0")
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "go build failed: %s", string(output))

			// Detect platform
			info, err := DetectBinaryPlatform(binaryPath)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedOS, info.OS)
			assert.Equal(t, tt.expectedArch, info.Arch)
			assert.Equal(t, tt.expectedType, info.Type)
		})
	}
}

func TestVerifyBinaryPlatform(t *testing.T) {
	// Create a simple Go binary for testing
	tempDir := t.TempDir()

	srcFile := filepath.Join(tempDir, "main.go")
	err := os.WriteFile(srcFile, []byte(`package main; func main() {}`), 0644)
	require.NoError(t, err)

	// Build for current platform
	binaryPath := filepath.Join(tempDir, "testbin")
	cmd := exec.Command("go", "build", "-o", binaryPath, srcFile)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", string(output))

	// Should pass for current platform
	err = VerifyBinaryPlatform(binaryPath, runtime.GOOS, runtime.GOARCH)
	assert.NoError(t, err)

	// Should fail for wrong OS
	err = VerifyBinaryPlatform(binaryPath, "fakeos", runtime.GOARCH)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OS mismatch")

	// Should fail for wrong arch
	err = VerifyBinaryPlatform(binaryPath, runtime.GOOS, "fakearch")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "arch mismatch")
}

func TestDetectBinaryPlatform_InvalidFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create a text file (not a binary)
	textFile := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(textFile, []byte("hello world"), 0644)
	require.NoError(t, err)

	info, err := DetectBinaryPlatform(textFile)
	require.NoError(t, err)
	assert.Equal(t, "unknown", info.Type)
}

func TestDetectBinaryPlatform_NonExistent(t *testing.T) {
	_, err := DetectBinaryPlatform("/nonexistent/path/binary")
	assert.Error(t, err)
}
