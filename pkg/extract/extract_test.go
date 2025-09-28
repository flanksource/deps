package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flanksource/clicky/task"
)

func TestExtractArchive(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "extract-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test with non-existent archive - should fail gracefully
	_, err = Extract("/nonexistent/archive.tar.gz", tempDir, (*task.Task)(nil), WithBinaryPath(""))
	if err == nil {
		t.Error("Expected error for non-existent archive")
	}
	if err != nil && !strings.Contains(err.Error(), "failed to extract archive") {
		t.Errorf("Expected 'failed to extract archive' in error, got: %v", err)
	}
}

func TestExtractFullArchive(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "extract-full-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test with non-existent archive - should fail gracefully
	_, err = Extract("/nonexistent/archive.tar.xz", tempDir, (*task.Task)(nil), WithFullExtract())
	if err == nil {
		t.Error("Expected error for non-existent archive")
	}
	if err != nil && !strings.Contains(err.Error(), "failed to extract archive") {
		t.Errorf("Expected 'failed to extract archive' in error, got: %v", err)
	}
}

func TestExtractDirectoryCreation(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "extract-dir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	nonExistentDir := filepath.Join(tempDir, "subdir")

	// This will fail at extraction, but should create the directory
	_, err = Extract("/nonexistent/archive.tar.gz", nonExistentDir, (*task.Task)(nil), WithBinaryPath(""))
	if err == nil {
		t.Error("Expected error for non-existent archive")
	}

	// Directory should still be created
	if _, statErr := os.Stat(nonExistentDir); statErr != nil {
		t.Errorf("Expected directory to be created, but got error: %v", statErr)
	}

	// Test that .tar.xz files are also handled (they'll fail extraction but go through the same path)
	_, err = Extract("/nonexistent/archive.tar.xz", tempDir, (*task.Task)(nil), WithBinaryPath(""))
	if err == nil {
		t.Error("Expected error for non-existent .tar.xz archive")
	}
	if err != nil && !strings.Contains(err.Error(), "failed to extract archive") {
		t.Errorf("Expected 'failed to extract archive' in error for .tar.xz, got: %v", err)
	}
}
