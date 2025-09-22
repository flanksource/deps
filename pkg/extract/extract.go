package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/commons/files"
)

// ExtractArchive extracts an archive and finds the binary inside
func ExtractArchive(archivePath, extractDir, binaryPath string, t *task.Task) (string, error) {
	// Convert to absolute paths for logging
	absArchivePath, _ := filepath.Abs(archivePath)
	absExtractDir, _ := filepath.Abs(extractDir)

	t.Debugf("Extract: starting extraction of %s to %s (binaryPath=%s)", absArchivePath, absExtractDir, binaryPath)

	// Ensure extract directory exists
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create extract directory: %w", err)
	}

	// Detect archive type and extract
	lowerArchivePath := strings.ToLower(archivePath)
	t.Debugf("Extract: detecting archive type for %s", absArchivePath)
	switch {
	case strings.HasSuffix(lowerArchivePath, ".tar.gz") || strings.HasSuffix(lowerArchivePath, ".tgz"):
		t.Debugf("Extract: extracting tar.gz archive %s", absArchivePath)
		if err := files.Untar(archivePath, extractDir); err != nil {
			return "", fmt.Errorf("failed to extract tar.gz: %w", err)
		}
		t.Debugf("Extract: tar.gz extraction completed for %s", absArchivePath)
	case strings.HasSuffix(lowerArchivePath, ".zip"):
		t.Debugf("Extract: extracting zip archive %s", absArchivePath)
		if err := files.Unzip(archivePath, extractDir); err != nil {
			return "", fmt.Errorf("failed to extract zip: %w", err)
		}
		t.Debugf("Extract: zip extraction completed for %s", absArchivePath)
	default:
		t.Debugf("Extract: unsupported archive type detected for %s", absArchivePath)
		return "", fmt.Errorf("unsupported archive type: %s", archivePath)
	}

	// Find the binary
	t.Debugf("Extract: searching for binary in %s (binaryPath=%s)", absExtractDir, binaryPath)
	return findBinaryInDir(extractDir, binaryPath, t)
}

// findBinaryInDir searches for the binary in the extracted directory
func findBinaryInDir(extractDir, binaryPath string, t *task.Task) (string, error) {
	absExtractDir, _ := filepath.Abs(extractDir)
	t.Debugf("Extract: findBinary starting search in %s for binaryPath=%s", absExtractDir, binaryPath)

	// If binary path is specified, try it first
	if binaryPath != "" {
		fullPath := filepath.Join(extractDir, binaryPath)
		absFullPath, _ := filepath.Abs(fullPath)
		t.Debugf("Extract: checking specified binary path %s", absFullPath)
		if fileExists(fullPath) {
			t.Debugf("Extract: found binary at specified path %s", absFullPath)
			return fullPath, nil
		}
		t.Debugf("Extract: specified binary path not found %s", absFullPath)

		// Try without directory structure (flat extraction)
		baseName := filepath.Base(binaryPath)
		flatPath := filepath.Join(extractDir, baseName)
		absFlatPath, _ := filepath.Abs(flatPath)
		t.Debugf("Extract: checking flat binary path %s", absFlatPath)
		if fileExists(flatPath) {
			t.Debugf("Extract: found binary at flat path %s", absFlatPath)
			return flatPath, nil
		}
		t.Debugf("Extract: flat binary path not found %s", absFlatPath)
	}

	// Search for executables in the directory
	t.Debugf("Extract: searching for executable files in %s", absExtractDir)
	var executables []string
	err := filepath.Walk(extractDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-executable files
		if info.IsDir() || info.Mode()&0111 == 0 {
			return nil
		}

		// Found an executable file
		absPath, _ := filepath.Abs(path)
		executables = append(executables, path)
		t.Debugf("Extract: found executable %s", absPath)

		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to search for executables: %w", err)
	}

	t.Debugf("Extract: found %d executable files in %s", len(executables), absExtractDir)

	if len(executables) == 0 {
		t.Debugf("Extract: no executable files found in %s", absExtractDir)
		return "", fmt.Errorf("no executable files found in archive")
	}

	// If only one executable, use it
	if len(executables) == 1 {
		absExecPath, _ := filepath.Abs(executables[0])
		t.Debugf("Extract: single executable found, using %s", absExecPath)
		return executables[0], nil
	}

	// Multiple executables - try to find the best match
	t.Debugf("Extract: multiple executables found (%d), searching for best match", len(executables))
	if binaryPath != "" {
		baseName := filepath.Base(binaryPath)
		for _, exec := range executables {
			if filepath.Base(exec) == baseName {
				absExecPath, _ := filepath.Abs(exec)
				t.Debugf("Extract: found matching executable by name %s", absExecPath)
				return exec, nil
			}
		}
	}

	// Return the first executable found
	absExecPath, _ := filepath.Abs(executables[0])
	t.Debugf("Extract: using first executable found %s", absExecPath)
	return executables[0], nil
}

// ExtractFullArchive extracts the full archive to a destination directory
func ExtractFullArchive(archivePath, extractDir string, t *task.Task) error {
	// Convert to absolute paths for logging
	absArchivePath, _ := filepath.Abs(archivePath)
	absExtractDir, _ := filepath.Abs(extractDir)

	t.Debugf("ExtractFull: starting full extraction of %s to %s", absArchivePath, absExtractDir)

	// Ensure extract directory exists
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("failed to create extract directory: %w", err)
	}

	// Detect archive type and extract
	lowerArchivePath := strings.ToLower(archivePath)
	t.Debugf("ExtractFull: detecting archive type for %s", absArchivePath)

	switch {
	case strings.HasSuffix(lowerArchivePath, ".tar.gz") || strings.HasSuffix(lowerArchivePath, ".tgz"):
		t.Debugf("ExtractFull: extracting tar.gz archive %s", absArchivePath)
		if err := files.Untar(archivePath, extractDir); err != nil {
			return fmt.Errorf("failed to extract tar.gz: %w", err)
		}
		t.Debugf("ExtractFull: tar.gz extraction completed for %s", absArchivePath)
	case strings.HasSuffix(lowerArchivePath, ".zip"):
		t.Debugf("ExtractFull: extracting zip archive %s", absArchivePath)
		if err := files.Unzip(archivePath, extractDir); err != nil {
			return fmt.Errorf("failed to extract zip: %w", err)
		}
		t.Debugf("ExtractFull: zip extraction completed for %s", absArchivePath)
	default:
		t.Debugf("ExtractFull: unsupported archive type detected for %s", absArchivePath)
		return fmt.Errorf("unsupported archive type: %s", archivePath)
	}

	t.Debugf("ExtractFull: full extraction completed for %s to %s", absArchivePath, absExtractDir)
	return nil
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
