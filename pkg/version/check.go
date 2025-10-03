package version

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/utils"
)

// GetInstalledVersion executes a binary with its version command and extracts the version
func GetInstalledVersion(t *task.Task, binaryPath, versionCommand, versionPattern string) (string, error) {
	return GetInstalledVersionWithMode(t, binaryPath, versionCommand, versionPattern, "")
}

// GetInstalledVersionWithMode executes a binary with its version command and extracts the version,
// supporting directory mode packages
func GetInstalledVersionWithMode(t *task.Task, binaryPath, versionCommand, versionPattern, mode string) (string, error) {
	if binaryPath == "" {
		return "", fmt.Errorf("binary path is empty")
	}

	if t != nil {
		t.V(3).Infof("Getting version for %s (mode: %s)", utils.LogPath(binaryPath), mode)
		if versionCommand != "" {
			t.V(4).Infof("Using custom version command: %s", versionCommand)
		}
		if versionPattern != "" {
			t.V(4).Infof("Using version pattern: %s", versionPattern)
		}
	}

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		if t != nil {
			t.V(4).Infof("Binary not found at %s", utils.LogPath(binaryPath))
		}
		return "", fmt.Errorf("binary not found: %s", binaryPath)
	}

	// Track if custom command was provided before defaulting
	wasCustomCommand := versionCommand != ""

	// Default version command if not specified
	if versionCommand == "" {
		versionCommand = "--version"
		if t != nil {
			t.V(4).Infof("Using default version command: %s", versionCommand)
		}
	}

	// Split command into parts
	cmdParts := strings.Fields(versionCommand)

	// Determine command strategy based on whether custom command was provided
	var versionCommands [][]string
	if wasCustomCommand {
		// Custom command specified - only try that exact command
		versionCommands = [][]string{cmdParts}
		if t != nil {
			t.V(3).Infof("Using custom version command only: %s", versionCommand)
		}
	} else {
		// No custom command - try variations as fallback
		versionCommands = [][]string{
			cmdParts,
			{"--version"},
			{"-v"},
			{"version"},
			{"-version"},
			{"--help"}, // Some tools only show version in help
		}
		if t != nil {
			t.V(3).Infof("Attempting version detection with %d command variations", len(versionCommands))
		}
	}

	// Set timeout to avoid hanging on interactive tools
	timeout := 10 * time.Second

	var output []byte
	var lastErr error

	for i, cmdArgs := range versionCommands {
		// Skip duplicate commands
		if i > 0 && len(cmdArgs) == len(cmdParts) {
			allMatch := true
			for j, arg := range cmdArgs {
				if j >= len(cmdParts) || arg != cmdParts[j] {
					allMatch = false
					break
				}
			}
			if allMatch {
				if t != nil {
					t.V(4).Infof("Skipping duplicate command: %v", cmdArgs)
				}
				continue
			}
		}

		if t != nil {
			t.V(4).Infof("Trying version command: %v", cmdArgs)
		}

		var cmd *exec.Cmd

		// Handle directory mode packages
		if mode == "directory" {
			if t != nil {
				t.V(4).Infof("Using directory mode for %s", utils.LogPath(binaryPath))
			}
			// For directory mode, check if there's exactly one item in the directory
			entries, err := os.ReadDir(binaryPath)
			if err != nil {
				lastErr = fmt.Errorf("failed to read directory %s: %w", binaryPath, err)
				if t != nil {
					t.V(4).Infof("Failed to read directory %s: %v", utils.LogPath(binaryPath), err)
				}
				continue
			}

			// Filter out hidden files (starting with '.')
			var visibleEntries []os.DirEntry
			for _, entry := range entries {
				if !strings.HasPrefix(entry.Name(), ".") {
					visibleEntries = append(visibleEntries, entry)
				}
			}

			if len(visibleEntries) == 1 {
				// Single item: cd into it and execute command
				singleItem := filepath.Join(binaryPath, visibleEntries[0].Name())
				cmd = exec.Command(cmdArgs[0], cmdArgs[1:]...)
				cmd.Dir = singleItem
				if t != nil {
					t.V(4).Infof("Single directory entry: executing in %s", utils.LogPath(singleItem))
				}
			} else {
				// Multiple items: stay in package directory and execute command
				cmd = exec.Command(cmdArgs[0], cmdArgs[1:]...)
				cmd.Dir = binaryPath
			}
		} else {
			// For binary mode, execute the binary directly
			cmd = exec.Command(binaryPath, cmdArgs...)
			if t != nil {
				t.V(4).Infof("Binary mode: executing %s %v", utils.LogPath(binaryPath), cmdArgs)
			}
		}

		done := make(chan error, 1)

		go func() {
			// Try combined output first (captures both stdout and stderr)
			out, err := cmd.CombinedOutput()
			if err != nil {
				// Check for fork/exec permission errors and provide better error messages
				if strings.Contains(err.Error(), "fork/exec") && strings.Contains(err.Error(), "permission denied") {
					// Determine the actual binary path for permission checking
					var checkPath string
					if mode == "directory" {
						// For directory mode, the binary path might be different
						checkPath = cmd.Path
					} else {
						checkPath = binaryPath
					}

					// Check if file exists and get permissions
					if info, statErr := os.Stat(checkPath); statErr == nil {
						enhancedErr := fmt.Errorf("binary %s exists but is not executable (permissions: %s). Try: chmod +x %s",
							utils.LogPath(checkPath), info.Mode().String(), checkPath)
						done <- enhancedErr
						return
					}
				}
				done <- err
				return
			}
			output = out
			done <- nil
		}()

		select {
		case err := <-done:
			if err == nil && len(output) > 0 {
				// Success! Break out of the loop
				if t != nil {
					t.V(4).Infof("Command succeeded, got %d bytes of output", len(output))
				}
				lastErr = nil
				goto parseOutput
			}
			if t != nil {
				t.V(4).Infof("Command failed: %v", err)
			}
			lastErr = err
		case <-time.After(timeout):
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			lastErr = fmt.Errorf("version command timed out after %v", timeout)
			if t != nil {
				t.V(4).Infof("Command timed out after %v", timeout)
			}
		}
	}

	// If we get here, all version commands failed
	if lastErr != nil {
		if t != nil {
			t.V(3).Infof("All version commands failed for %s", utils.LogPath(binaryPath))
		}
		return "", fmt.Errorf("all version commands failed, last error: %v", lastErr)
	}

parseOutput:
	if t != nil {
		t.V(3).Infof("Parsing version output for %s: %s", utils.LogPath(binaryPath), strings.TrimSpace(string(output)))
	}

	// Extract version from output
	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		if t != nil {
			t.V(4).Infof("No output from version command")
		}
		return "", fmt.Errorf("no output from version command")
	}

	if t != nil {
		// Limit output logging to first few lines to avoid spam
		lines := strings.Split(outputStr, "\n")
		if len(lines) > 3 {
			t.V(4).Infof("Version output (first 3 lines): %s...", strings.Join(lines[:3], " | "))
		} else {
			t.V(4).Infof("Version output: %s", strings.ReplaceAll(outputStr, "\n", " | "))
		}
	}

	// Extract version using pattern
	version, err := ExtractFromOutput(outputStr, versionPattern)
	if err != nil {
		if t != nil {
			t.V(4).Infof("Initial pattern extraction failed, trying permissive approach")
		}
		// If pattern extraction fails, try with a more permissive approach
		// Look for common version patterns in the output
		lines := strings.Split(outputStr, "\n")
		for lineNum, line := range lines {
			if version, err := ExtractFromOutput(line, ""); err == nil {
				if t != nil {
					t.V(4).Infof("Found version on line %d: %s", lineNum+1, version)
				}
				return version, nil
			}
		}
		if t != nil {
			t.V(3).Infof("Failed to extract version from any output line")
		}
		return "", fmt.Errorf("failed to extract version from output: %w\nOutput: %s", err, outputStr)
	}

	if t != nil {
		t.V(3).Infof("Successfully extracted version: %s", version)
	}
	return version, nil
}

// CheckBinaryVersion checks the version of a binary against expected versions
func CheckBinaryVersion(t *task.Task, tool string, pkg types.Package, binDir string, expectedVersion, requestedVersion string) types.CheckResult {
	result := types.CheckResult{
		Tool:             tool,
		ExpectedVersion:  expectedVersion,
		RequestedVersion: requestedVersion,
	}

	if t != nil {
		t.V(3).Infof("Checking version for %s (expected: %s, requested: %s)", tool, expectedVersion, requestedVersion)
	}

	// Determine binary/directory name - prioritize pkg.Name over tool parameter
	targetName := tool
	if pkg.Name != "" {
		targetName = pkg.Name
		if t != nil {
			t.V(4).Infof("Using package name: %s (instead of %s)", targetName, tool)
		}
	}

	// Determine binary path
	binaryPath := filepath.Join(binDir, targetName)

	// Handle directory mode packages
	if pkg.Mode == "directory" {
		if t != nil {
			t.V(4).Infof("Using directory mode for %s at %s", targetName, utils.LogPath(binaryPath))
		}
		// For directory mode, binaryPath should be the package directory
		if stat, err := os.Stat(binaryPath); err == nil && stat.IsDir() {
			// binaryPath is already correct for directory mode
			if t != nil {
				t.V(4).Infof("Directory found: %s", utils.LogPath(binaryPath))
			}
		} else {
			result.Status = types.CheckStatusMissing
			result.Error = fmt.Sprintf("Package directory not found: %s", binaryPath)
			if t != nil {
				t.V(3).Infof("Package directory not found: %s", utils.LogPath(binaryPath))
			}
			return result
		}
	} else {
		// Handle Windows executables for binary mode
		if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
			binaryPath = binaryPath + ".exe"
			if t != nil {
				t.V(4).Infof("Trying Windows executable: %s", utils.LogPath(binaryPath))
			}
		}
	}

	// Special handling for postgres (directory structure)
	if strings.ToLower(tool) == "postgres" || strings.Contains(strings.ToLower(tool), "postgres") {
		if t != nil {
			t.V(4).Infof("Special postgres handling for %s", tool)
		}
		postgresDir := filepath.Join(binDir, tool)
		if stat, err := os.Stat(postgresDir); err == nil && stat.IsDir() {
			// Look for postgres binary inside the directory
			possiblePaths := []string{
				filepath.Join(postgresDir, "bin", "postgres"),
				filepath.Join(postgresDir, "bin", "postgres.exe"),
				filepath.Join(postgresDir, "postgres"),
				filepath.Join(postgresDir, "postgres.exe"),
			}

			if t != nil {
				t.V(4).Infof("Looking for postgres binary in %d possible locations", len(possiblePaths))
			}

			for _, path := range possiblePaths {
				if _, err := os.Stat(path); err == nil {
					binaryPath = path
					if t != nil {
						t.V(4).Infof("Found postgres binary at %s", utils.LogPath(path))
					}
					break
				}
			}
		}
	}

	result.BinaryPath = binaryPath

	// Check if binary/directory exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		result.Status = types.CheckStatusMissing
		if pkg.Mode == "directory" {
			result.Error = fmt.Sprintf("Package directory not found: %s", binaryPath)
		} else {
			result.Error = fmt.Sprintf("Binary not found: %s", binaryPath)
		}
		if t != nil {
			t.V(3).Infof("Binary/directory not found: %s", utils.LogPath(binaryPath))
		}
		return result
	}

	// Get installed version
	installedVersion, err := GetInstalledVersionWithMode(t, binaryPath, pkg.VersionCommand, pkg.VersionPattern, pkg.Mode)
	if err != nil {
		result.Status = types.CheckStatusError
		result.Error = fmt.Sprintf("Failed to get version: %v", err)
		if t != nil {
			t.V(3).Infof("Failed to get version for %s: %v", tool, err)
		}
		return result
	}

	result.InstalledVersion = installedVersion

	if t != nil {
		t.V(3).Infof("Found installed version %s for %s", installedVersion, tool)
	}

	// If no expected version, we can only report what's installed
	if expectedVersion == "" && requestedVersion == "" {
		result.Status = types.CheckStatusUnknown
		if t != nil {
			t.V(4).Infof("No expected version to compare against")
		}
		return result
	}

	// Compare versions
	compareVersion := expectedVersion
	if compareVersion == "" {
		compareVersion = requestedVersion
	}

	if t != nil {
		t.V(3).Infof("Comparing versions: installed=%s vs expected=%s", installedVersion, compareVersion)
	}

	// Normalize versions for comparison
	normalizedInstalled := Normalize(installedVersion)
	normalizedExpected := Normalize(compareVersion)

	if t != nil {
		t.V(4).Infof("Normalized versions: installed=%s vs expected=%s", normalizedInstalled, normalizedExpected)
	}

	if normalizedInstalled == normalizedExpected {
		result.Status = types.CheckStatusOK
		if t != nil {
			t.V(3).Infof("Version match: %s == %s (normalized)", installedVersion, compareVersion)
		}
		return result
	}

	// Try semantic version comparison
	cmp, err := Compare(installedVersion, compareVersion)
	if err != nil {
		if t != nil {
			t.V(4).Infof("Semantic version comparison failed: %v, falling back to string comparison", err)
		}
		// If semantic comparison fails, use string comparison
		if normalizedInstalled != normalizedExpected {
			result.Status = types.CheckStatusOutdated
		} else {
			result.Status = types.CheckStatusOK
		}
		return result
	}

	switch {
	case cmp == 0:
		result.Status = types.CheckStatusOK
		if t != nil {
			t.V(3).Infof("Version match: %s == %s (semantic)", installedVersion, compareVersion)
		}
	case cmp > 0:
		// Installed version is newer than expected - this is usually OK
		result.Status = types.CheckStatusNewer
		if t != nil {
			t.V(3).Infof("Newer version installed: %s > %s", installedVersion, compareVersion)
		}
	case cmp < 0:
		// Installed version is older than expected
		result.Status = types.CheckStatusOutdated
		if t != nil {
			t.V(3).Infof("Outdated version: %s < %s", installedVersion, compareVersion)
		}
	}

	return result
}

// ScanBinDirectory scans the bin directory for installed tools
func ScanBinDirectory(binDir string) ([]string, error) {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read bin directory %s: %w", binDir, err)
	}

	var tools []string
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files and certain patterns
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".tmp") {
			continue
		}

		// Keep the full name - don't strip .exe or skip directories
		// The version checking logic will handle all entry types appropriately
		tools = append(tools, name)
	}

	return tools, nil
}

// CheckExistingInstallation checks if a binary exists and matches the requested version
// Returns the existing version if it matches, empty string otherwise
func CheckExistingInstallation(t *task.Task, name string, pkg types.Package, requestedVersion, binDir string, osOverride string) string {
	if t != nil {
		t.V(3).Infof("Checking existing installation of %s (requested: %s)", name, requestedVersion)
	}

	// Determine binary path - handle directory mode
	var binaryPath string

	if pkg.Mode == "directory" {
		// For directory mode, determine directory name with same logic as installer
		dirName := name
		if pkg.Name != "" {
			dirName = pkg.Name
			if t != nil {
				t.V(4).Infof("Using package name for directory: %s", dirName)
			}
		}

		binaryPath = filepath.Join(binDir, dirName)
		if t != nil {
			t.V(4).Infof("Directory mode: checking %s", utils.LogPath(binaryPath))
		}
		// Check if directory exists
		if stat, err := os.Stat(binaryPath); os.IsNotExist(err) || !stat.IsDir() {
			if t != nil {
				t.V(4).Infof("Directory not found or not a directory: %s", utils.LogPath(binaryPath))
			}
			return ""
		}
	} else {
		// Determine binary name - prioritize pkg.Name, then pkg.BinaryName
		binaryName := name
		if pkg.Name != "" {
			binaryName = pkg.Name
			if t != nil {
				t.V(4).Infof("Using package name: %s", binaryName)
			}
		}
		if pkg.BinaryName != "" {
			binaryName = pkg.BinaryName
			if t != nil {
				t.V(4).Infof("Using custom binary name: %s", binaryName)
			}
		}

		// For Windows, add .exe extension if not present
		if filepath.Ext(binaryName) == "" && (osOverride == "windows") {
			binaryName += ".exe"
			if t != nil {
				t.V(4).Infof("Windows OS: using %s", binaryName)
			}
		}

		binaryPath = filepath.Join(binDir, binaryName)
		if t != nil {
			t.V(4).Infof("Binary mode: checking %s", utils.LogPath(binaryPath))
		}

		// Check if binary exists
		if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
			if t != nil {
				t.V(4).Infof("Binary not found: %s", utils.LogPath(binaryPath))
			}
			return ""
		}
	}

	// Try to get the installed version
	installedVersion, err := GetInstalledVersionWithMode(t, binaryPath, pkg.VersionCommand, pkg.VersionPattern, pkg.Mode)
	if err != nil {
		if t != nil {
			t.V(4).Infof("Failed to get installed version: %v", err)
		}
		return ""
	}

	// Normalize both versions for comparison
	normalizedInstalled := Normalize(installedVersion)
	normalizedRequested := Normalize(requestedVersion)

	if t != nil {
		t.V(4).Infof("Version comparison: installed=%s (%s) vs requested=%s (%s)",
			installedVersion, normalizedInstalled, requestedVersion, normalizedRequested)
	}

	if normalizedInstalled == normalizedRequested {
		if t != nil {
			t.V(3).Infof("Existing installation matches requested version: %s", installedVersion)
		}
		return installedVersion
	}

	if t != nil {
		t.V(3).Infof("Existing installation version mismatch: %s != %s", installedVersion, requestedVersion)
	}
	return ""
}
