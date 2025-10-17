package version

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/flanksource/deps/pkg/types"
)

// getVersionCommandVariations returns command variations to try based on whether custom command was provided
func getVersionCommandVariations(wasCustomCommand bool, cmdParts []string) [][]string {
	if wasCustomCommand {
		return [][]string{cmdParts}
	}

	return [][]string{
		cmdParts,
		{"--version"},
		{"-v"},
		{"version"},
		{"-version"},
		{"--help"}, // Some tools only show version in help
	}
}

// isDuplicateCommand checks if cmdArgs is a duplicate of the original command parts
func isDuplicateCommand(cmdArgs, original []string) bool {
	if len(cmdArgs) != len(original) {
		return false
	}

	for j, arg := range cmdArgs {
		if j >= len(original) || arg != original[j] {
			return false
		}
	}
	return true
}

// getVisibleEntries filters out hidden files (starting with '.') from a directory
func getVisibleEntries(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var visibleEntries []os.DirEntry
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			visibleEntries = append(visibleEntries, entry)
		}
	}

	return visibleEntries, nil
}

// parseVersionOutput extracts version from command output using the provided pattern
func parseVersionOutput(output string, versionPattern string) (string, error) {
	outputStr := strings.TrimSpace(output)
	if outputStr == "" {
		return "", fmt.Errorf("no output from version command")
	}

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

// getShellBinary returns "bash" if available, otherwise "sh"
func getShellBinary() string {
	if _, err := exec.LookPath("bash"); err == nil {
		return "bash"
	}
	return "sh"
}

// compareVersions compares installed version against expected version
// Returns the appropriate CheckStatus and any error
func compareVersions(installed, expected string) (types.CheckStatus, error) {
	// Normalize versions for comparison
	normalizedInstalled := Normalize(installed)
	normalizedExpected := Normalize(expected)

	if normalizedInstalled == normalizedExpected {
		return types.CheckStatusOK, nil
	}

	// Try semantic version comparison
	cmp, err := Compare(installed, expected)
	if err != nil {
		// If semantic comparison fails, use string comparison
		if normalizedInstalled != normalizedExpected {
			return types.CheckStatusOutdated, nil
		}
		return types.CheckStatusOK, nil
	}

	switch {
	case cmp == 0:
		return types.CheckStatusOK, nil
	case cmp > 0:
		return types.CheckStatusNewer, nil
	case cmp < 0:
		return types.CheckStatusOutdated, nil
	}

	return types.CheckStatusUnknown, nil
}
