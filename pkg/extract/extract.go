package extract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/system"
	"github.com/flanksource/deps/pkg/utils"
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

// IsSystemInstaller returns true if the file is a system installer
func IsSystemInstaller(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".pkg" || ext == ".msi"
}

// HandleInstaller processes system installers (.pkg/.msi) and returns the installed binary path
func HandleInstaller(installerPath, toolName string, t *task.Task) (string, error) {
	if !IsSystemInstaller(installerPath) {
		return "", fmt.Errorf("file is not a system installer: %s", installerPath)
	}

	opts := &system.SystemInstallOptions{
		ToolName: toolName,
		Silent:   false, // Always show warnings for system installations
		Task:     t,
	}

	result, err := system.InstallSystemPackage(installerPath, "", opts)
	if err != nil {
		return "", fmt.Errorf("failed to install %s: %w", toolName, err)
	}

	// Return the path to the installed binary
	return result.BinaryPath, nil
}

// Extract extracts an archive with optional configuration
// Returns the path to the binary if searching for one, or empty string if full extract
func Extract(archivePath, extractDir string, t *task.Task, opts ...ExtractOption) (string, error) {
	// Parse options
	config := &extractConfig{}
	for _, opt := range opts {
		opt(config)
	}

	if t != nil {
		if config.fullExtract {
			t.SetDescription(fmt.Sprintf("Extracting %s", filepath.Base(archivePath)))
		} else {
			t.SetDescription(fmt.Sprintf("Extracting %s for %s", filepath.Base(archivePath), config.binaryPath))
		}
	}

	// Remove extraction directory if it exists to avoid permission issues from previous failed runs
	if _, err := os.Stat(extractDir); err == nil {
		if err := os.RemoveAll(extractDir); err != nil {
			return "", fmt.Errorf("failed to clean up existing extract directory: %w", err)
		}
	}

	// Create fresh extract directory
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create extract directory: %w", err)
	}

	// Extract archive using files.Unarchive which supports all formats
	extractResult, err := Unarchive(archivePath, extractDir, WithOverwrite(true))
	if err != nil {
		return "", fmt.Errorf("failed to extract archive: %w", err)
	}
	if t != nil {
		t.Debugf("Extracted %d files from %s", len(extractResult.Files), filepath.Base(archivePath))
	}

	// Verify extraction destination
	if err := verifyExtraction(extractDir, t); err != nil {
		return "", fmt.Errorf("extraction verification failed: %w", err)
	}

	// For full extraction, we're done
	if config.fullExtract {
		return "", nil
	}

	// Find the binary
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

	// If binary path is specified, try it first
	if binaryPath != "" {
		fullPath := filepath.Join(extractDir, binaryPath)
		if fileExists(fullPath) {
			utils.LogBinarySearch(t, extractDir, binaryPath, true, fullPath)
			return fullPath, nil
		}

		// Try without directory structure (flat extraction)
		baseName := filepath.Base(binaryPath)
		flatPath := filepath.Join(extractDir, baseName)
		if fileExists(flatPath) {
			utils.LogBinarySearch(t, extractDir, binaryPath, true, flatPath)
			return flatPath, nil
		}
	}

	// Search for executables in the directory
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
		executables = append(executables, path)

		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to search for executables: %w", err)
	}

	if len(executables) == 0 {
		utils.LogBinarySearch(t, extractDir, binaryPath, false, "")
		return "", fmt.Errorf("no executable files found in archive")
	}

	// If only one executable, use it
	if len(executables) == 1 {
		utils.LogBinarySearch(t, extractDir, "executable", true, executables[0])
		return executables[0], nil
	}

	// Multiple executables - try to find the best match
	if t != nil {
		t.Debugf("Found %d executables, searching for best match", len(executables))
	}
	if binaryPath != "" {
		baseName := filepath.Base(binaryPath)
		for _, exec := range executables {
			if filepath.Base(exec) == baseName {
				utils.LogBinarySearch(t, extractDir, binaryPath, true, exec)
				return exec, nil
			}
		}
	}

	// Return the first executable found
	utils.LogBinarySearch(t, extractDir, "first executable", true, executables[0])
	return executables[0], nil
}

// fileExists checks if a file exists and is not a directory
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// verifyExtraction verifies that extraction destination exists and is not empty
func verifyExtraction(extractDir string, t *task.Task) error {

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

	return nil
}

// verifyBinary verifies that a binary file exists, is not empty, and is executable
func verifyBinary(binaryPath string, t *task.Task) error {

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
		t.Debugf("Binary verified: %s", utils.FormatFileInfo(binaryPath))
	}

	return nil
}
