package extract

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/commons/files"
)

// ExtractOption is a functional option for configuring extraction
type ExtractOption func(*extractConfig)

// extractConfig holds configuration for extraction
type extractConfig struct {
	binaryPath  string // Path to specific binary to find
	fullExtract bool   // Extract full archive without searching for binary
}

// WithBinaryPath sets the path to the binary to find in the archive
func WithBinaryPath(binaryPath string) ExtractOption {
	return func(c *extractConfig) {
		c.binaryPath = binaryPath
	}
}

// WithFullExtract configures the extraction to extract the full archive
func WithFullExtract() ExtractOption {
	return func(c *extractConfig) {
		c.fullExtract = true
	}
}

// Extract extracts an archive with optional configuration
// Returns the path to the binary if searching for one, or empty string if full extract
func Extract(archivePath, extractDir string, t *task.Task, opts ...ExtractOption) (string, error) {
	// Parse options
	config := &extractConfig{}
	for _, opt := range opts {
		opt(config)
	}

	// Convert to absolute paths for logging
	absArchivePath, _ := filepath.Abs(archivePath)
	absExtractDir, _ := filepath.Abs(extractDir)

	if t != nil {
		if config.fullExtract {
			t.Debugf("Extract: starting full extraction of %s to %s", absArchivePath, absExtractDir)
		} else {
			t.Debugf("Extract: starting extraction of %s to %s (binaryPath=%s)", absArchivePath, absExtractDir, config.binaryPath)
		}
	}

	// Ensure extract directory exists
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create extract directory: %w", err)
	}

	// Extract archive using files.Unarchive which supports all formats
	if t != nil {
		t.Debugf("Extract: extracting archive %s to destination %s", absArchivePath, absExtractDir)
	}
	extractResult, err := files.Unarchive(archivePath, extractDir, files.WithOverwrite(true))
	if err != nil {
		return "", fmt.Errorf("failed to extract archive: %w", err)
	}
	if t != nil {
		t.Debugf("Extract: extraction completed for %s to %s (%d files extracted)", absArchivePath, absExtractDir, len(extractResult.Files))
	}

	// Verify extraction destination
	if err := verifyExtraction(extractDir, t); err != nil {
		return "", fmt.Errorf("extraction verification failed: %w", err)
	}

	// For full extraction, we're done
	if config.fullExtract {
		if t != nil {
			t.Debugf("Extract: full extraction completed successfully from %s to destination %s", absArchivePath, absExtractDir)
		}
		return "", nil
	}

	// Find the binary
	if t != nil {
		t.Debugf("Extract: searching for binary in destination %s (binaryPath=%s)", absExtractDir, config.binaryPath)
	}
	binaryPath, err := FindBinaryInDir(extractDir, config.binaryPath, t)
	if err != nil {
		return "", err
	}

	// Verify found binary
	if binaryPath != "" {
		if err := verifyBinary(binaryPath, t); err != nil {
			return "", fmt.Errorf("binary verification failed: %w", err)
		}
	}

	return binaryPath, nil
}


// FindBinaryInDir searches for the binary in the extracted directory
func FindBinaryInDir(extractDir, binaryPath string, t *task.Task) (string, error) {
	absExtractDir, _ := filepath.Abs(extractDir)
	if t != nil {
		t.Debugf("Extract: findBinary starting search in destination %s for binaryPath=%s", absExtractDir, binaryPath)
	}

	// If binary path is specified, try it first
	if binaryPath != "" {
		fullPath := filepath.Join(extractDir, binaryPath)
		absFullPath, _ := filepath.Abs(fullPath)
		if t != nil {
			t.Debugf("Extract: checking specified binary path %s", absFullPath)
		}
		if fileExists(fullPath) {
			if info, err := os.Stat(fullPath); err == nil {
				if t != nil {
					t.Debugf("Extract: found binary at specified destination %s (size: %d bytes, mode: %o)", absFullPath, info.Size(), info.Mode())
				}
			} else if t != nil {
				t.Debugf("Extract: found binary at specified destination %s", absFullPath)
			}
			return fullPath, nil
		}
		if t != nil {
			t.Debugf("Extract: specified binary path not found %s", absFullPath)
		}

		// Try without directory structure (flat extraction)
		baseName := filepath.Base(binaryPath)
		flatPath := filepath.Join(extractDir, baseName)
		absFlatPath, _ := filepath.Abs(flatPath)
		if t != nil {
			t.Debugf("Extract: checking flat binary path %s", absFlatPath)
		}
		if fileExists(flatPath) {
			if info, err := os.Stat(flatPath); err == nil {
				if t != nil {
					t.Debugf("Extract: found binary at flat destination %s (size: %d bytes, mode: %o)", absFlatPath, info.Size(), info.Mode())
				}
			} else if t != nil {
				t.Debugf("Extract: found binary at flat destination %s", absFlatPath)
			}
			return flatPath, nil
		}
		if t != nil {
			t.Debugf("Extract: flat binary path not found %s", absFlatPath)
		}
	}

	// Search for executables in the directory
	if t != nil {
		t.Debugf("Extract: searching for executable files in destination %s", absExtractDir)
	}
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
		if t != nil {
			t.Debugf("Extract: found executable at destination %s (size: %d bytes, mode: %o)", absPath, info.Size(), info.Mode())
		}

		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to search for executables: %w", err)
	}

	if t != nil {
		t.Debugf("Extract: found %d executable files in destination %s", len(executables), absExtractDir)
	}

	if len(executables) == 0 {
		if t != nil {
			t.Debugf("Extract: no executable files found in destination %s", absExtractDir)
		}
		return "", fmt.Errorf("no executable files found in archive")
	}

	// If only one executable, use it
	if len(executables) == 1 {
		absExecPath, _ := filepath.Abs(executables[0])
		if info, err := os.Stat(executables[0]); err == nil {
			if t != nil {
				t.Debugf("Extract: single executable found at destination %s (size: %d bytes, mode: %o)", absExecPath, info.Size(), info.Mode())
			}
		} else if t != nil {
			t.Debugf("Extract: single executable found at destination %s", absExecPath)
		}
		return executables[0], nil
	}

	// Multiple executables - try to find the best match
	if t != nil {
		t.Debugf("Extract: multiple executables found (%d), searching for best match", len(executables))
	}
	if binaryPath != "" {
		baseName := filepath.Base(binaryPath)
		for _, exec := range executables {
			if filepath.Base(exec) == baseName {
				absExecPath, _ := filepath.Abs(exec)
				if info, err := os.Stat(exec); err == nil {
					if t != nil {
						t.Debugf("Extract: found matching executable by name at destination %s (size: %d bytes, mode: %o)", absExecPath, info.Size(), info.Mode())
					}
				} else if t != nil {
					t.Debugf("Extract: found matching executable by name at destination %s", absExecPath)
				}
				return exec, nil
			}
		}
	}

	// Return the first executable found
	absExecPath, _ := filepath.Abs(executables[0])
	if info, err := os.Stat(executables[0]); err == nil {
		if t != nil {
			t.Debugf("Extract: using first executable found at destination %s (size: %d bytes, mode: %o)", absExecPath, info.Size(), info.Mode())
		}
	} else if t != nil {
		t.Debugf("Extract: using first executable found at destination %s", absExecPath)
	}
	return executables[0], nil
}


// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// verifyExtraction verifies that extraction destination exists and is not empty
func verifyExtraction(extractDir string, t *task.Task) error {
	absExtractDir, _ := filepath.Abs(extractDir)

	if t != nil {
		t.Debugf("Extract: verifying extraction destination %s", absExtractDir)
	}

	// Check if extraction directory exists
	info, err := os.Stat(extractDir)
	if err != nil {
		return fmt.Errorf("extraction destination does not exist: %s", extractDir)
	}

	if !info.IsDir() {
		return fmt.Errorf("extraction destination is not a directory: %s", extractDir)
	}

	// Check if directory contains files
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return fmt.Errorf("failed to read extraction destination: %w", err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("extraction destination is empty: %s", extractDir)
	}

	// Count files vs directories
	var fileCount, dirCount int
	var totalSize int64
	for _, entry := range entries {
		if entry.IsDir() {
			dirCount++
		} else {
			fileCount++
			if info, err := entry.Info(); err == nil {
				totalSize += info.Size()
			}
		}
	}

	if t != nil {
		t.Debugf("Extract: destination verified %s (%d files, %d directories, %d bytes total)",
			absExtractDir, fileCount, dirCount, totalSize)
	}

	return nil
}

// verifyBinary verifies that a binary file exists, is not empty, and is executable
func verifyBinary(binaryPath string, t *task.Task) error {
	absBinaryPath, _ := filepath.Abs(binaryPath)

	if t != nil {
		t.Debugf("Extract: verifying binary %s", absBinaryPath)
	}

	// Check if binary file exists
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("binary does not exist: %s", binaryPath)
	}

	// Check if it's a regular file (not a directory)
	if info.IsDir() {
		return fmt.Errorf("binary path is a directory, not a file: %s", binaryPath)
	}

	// Check if file is not empty
	if info.Size() == 0 {
		return fmt.Errorf("binary file is empty: %s", binaryPath)
	}

	// Check if file has execute permissions
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("binary file is not executable (mode: %o): %s", info.Mode(), binaryPath)
	}

	if t != nil {
		t.Debugf("Extract: binary verified %s (size: %d bytes, mode: %o, executable: true)",
			absBinaryPath, info.Size(), info.Mode())
	}

	return nil
}
