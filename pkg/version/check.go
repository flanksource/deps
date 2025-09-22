package version

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/deps/pkg/types"
)

// GetInstalledVersion executes a binary with its version command and extracts the version
func GetInstalledVersion(binaryPath, versionCommand, versionPattern string) (string, error) {
	if binaryPath == "" {
		return "", fmt.Errorf("binary path is empty")
	}

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "", fmt.Errorf("binary not found: %s", binaryPath)
	}

	// Default version command if not specified
	if versionCommand == "" {
		versionCommand = "--version"
	}

	// Split command into parts
	cmdParts := strings.Fields(versionCommand)

	// Try multiple version commands if the default fails
	versionCommands := [][]string{
		cmdParts,
		{"--version"},
		{"-v"},
		{"version"},
		{"-version"},
		{"--help"}, // Some tools only show version in help
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
				continue
			}
		}

		cmd := exec.Command(binaryPath, cmdArgs...)
		done := make(chan error, 1)

		go func() {
			// Try combined output first (captures both stdout and stderr)
			out, err := cmd.CombinedOutput()
			if err != nil {
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
				lastErr = nil
				goto parseOutput
			}
			lastErr = err
		case <-time.After(timeout):
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			lastErr = fmt.Errorf("version command timed out after %v", timeout)
		}
	}

	// If we get here, all version commands failed
	if lastErr != nil {
		return "", fmt.Errorf("all version commands failed, last error: %v", lastErr)
	}

parseOutput:

	// Extract version from output
	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return "", fmt.Errorf("no output from version command")
	}

	// Extract version using pattern
	version, err := ExtractFromOutput(outputStr, versionPattern)
	if err != nil {
		// If pattern extraction fails, try with a more permissive approach
		// Look for common version patterns in the output
		lines := strings.Split(outputStr, "\n")
		for _, line := range lines {
			if version, err := ExtractFromOutput(line, ""); err == nil {
				return version, nil
			}
		}
		return "", fmt.Errorf("failed to extract version from output: %w\nOutput: %s", err, outputStr)
	}

	return version, nil
}

// CheckBinaryVersion checks the version of a binary against expected versions
func CheckBinaryVersion(tool string, pkg types.Package, binDir string, expectedVersion, requestedVersion string) types.CheckResult {
	result := types.CheckResult{
		Tool:             tool,
		ExpectedVersion:  expectedVersion,
		RequestedVersion: requestedVersion,
	}

	// Determine binary path
	binaryPath := filepath.Join(binDir, tool)

	// Handle Windows executables
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		binaryPath = binaryPath + ".exe"
	}

	// Special handling for postgres (directory structure)
	if strings.ToLower(tool) == "postgres" || strings.Contains(strings.ToLower(tool), "postgres") {
		postgresDir := filepath.Join(binDir, tool)
		if stat, err := os.Stat(postgresDir); err == nil && stat.IsDir() {
			// Look for postgres binary inside the directory
			possiblePaths := []string{
				filepath.Join(postgresDir, "bin", "postgres"),
				filepath.Join(postgresDir, "bin", "postgres.exe"),
				filepath.Join(postgresDir, "postgres"),
				filepath.Join(postgresDir, "postgres.exe"),
			}

			for _, path := range possiblePaths {
				if _, err := os.Stat(path); err == nil {
					binaryPath = path
					break
				}
			}
		}
	}

	result.BinaryPath = binaryPath

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		result.Status = types.CheckStatusMissing
		result.Error = fmt.Sprintf("Binary not found: %s", binaryPath)
		return result
	}

	// Get installed version
	installedVersion, err := GetInstalledVersion(binaryPath, pkg.VersionCommand, pkg.VersionPattern)
	if err != nil {
		result.Status = types.CheckStatusError
		result.Error = fmt.Sprintf("Failed to get version: %v", err)
		return result
	}

	result.InstalledVersion = installedVersion

	// If no expected version, we can only report what's installed
	if expectedVersion == "" && requestedVersion == "" {
		result.Status = types.CheckStatusUnknown
		return result
	}

	// Compare versions
	compareVersion := expectedVersion
	if compareVersion == "" {
		compareVersion = requestedVersion
	}

	// Normalize versions for comparison
	normalizedInstalled := Normalize(installedVersion)
	normalizedExpected := Normalize(compareVersion)

	if normalizedInstalled == normalizedExpected {
		result.Status = types.CheckStatusOK
		return result
	}

	// Try semantic version comparison
	cmp, err := Compare(installedVersion, compareVersion)
	if err != nil {
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
	case cmp > 0:
		// Installed version is newer than expected - this is usually OK
		result.Status = types.CheckStatusNewer
	case cmp < 0:
		// Installed version is older than expected
		result.Status = types.CheckStatusOutdated
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

		// Remove .exe extension for Windows binaries
		if strings.HasSuffix(name, ".exe") {
			name = strings.TrimSuffix(name, ".exe")
		}

		tools = append(tools, name)
	}

	return tools, nil
}