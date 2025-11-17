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

// binaryPathInfo holds information about resolved binary path and version checking strategy
type binaryPathInfo struct {
	BinaryPath     string
	VersionCommand string
	Mode           string
}

// ContainsShellOperators checks if a command contains shell-specific operators
// that require wrapping in a shell (bash -c or sh -c)
func ContainsShellOperators(cmd string) bool {
	shellOps := []string{"|", ">", "<", "2>", "&&", "||", ";", "`", "$("}
	for _, op := range shellOps {
		if strings.Contains(cmd, op) {
			return true
		}
	}
	return false
}

// ResolveVersionCommandBinary resolves the binary path from a version command
// Returns the resolved path and whether it was found
func ResolveVersionCommandBinary(versionCommand, binaryPath, binDir, mode string) (string, bool) {
	cmdParts := strings.Fields(versionCommand)
	if len(cmdParts) == 0 {
		return "", false
	}

	firstToken := cmdParts[0]

	// Check if it's a relative path (contains / or starts with ./)
	if strings.Contains(firstToken, "/") {
		var resolvedPath string
		if mode == "directory" {
			resolvedPath = filepath.Join(binaryPath, firstToken)
		} else {
			resolvedPath = filepath.Join(binDir, firstToken)
		}

		if info, err := os.Stat(resolvedPath); err == nil {
			if mode != "directory" && info.Mode()&0111 == 0 {
				return "", false
			}
			return resolvedPath, true
		}
		return "", false
	}

	// Try to find on PATH
	if pathBinary, err := exec.LookPath(firstToken); err == nil {
		return pathBinary, true
	}

	// Try in binDir as a fallback
	binDirPath := filepath.Join(binDir, firstToken)
	if info, err := os.Stat(binDirPath); err == nil {
		if info.Mode()&0111 != 0 {
			return binDirPath, true
		}
	}

	return "", false
}

// resolveBinaryPath determines the binary path, version command, and mode
// for a given package, consolidating logic from CheckBinaryVersion and CheckExistingInstallation
func resolveBinaryPath(tool string, pkg types.Package, binDir string, osOverride string) (binaryPathInfo, error) {
	targetName := tool
	if pkg.Name != "" {
		targetName = pkg.Name
	}

	// For directory mode packages with symlinks, use the symlink for version checking
	if pkg.Mode == "directory" && len(pkg.Symlinks) > 0 && pkg.VersionCommand != "" {
		cmdParts := strings.Fields(pkg.VersionCommand)
		if len(cmdParts) > 0 {
			symlinkName := filepath.Base(cmdParts[0])
			binaryPath := filepath.Join(binDir, symlinkName)

			versionCommand := ""
			if len(cmdParts) > 1 {
				versionCommand = strings.Join(cmdParts[1:], " ")
			}

			return binaryPathInfo{
				BinaryPath:     binaryPath,
				VersionCommand: versionCommand,
				Mode:           "", // Use binary mode for symlinks
			}, nil
		}
	}

	if pkg.Mode == "directory" {
		return binaryPathInfo{
			BinaryPath:     filepath.Join(binDir, targetName),
			VersionCommand: pkg.VersionCommand,
			Mode:           "directory",
		}, nil
	}

	// Binary mode (default)
	binaryName := targetName
	if pkg.BinaryName != "" {
		binaryName = pkg.BinaryName
	}

	// For Windows, add .exe extension if not present
	if filepath.Ext(binaryName) == "" && osOverride == "windows" {
		binaryName += ".exe"
	}

	return binaryPathInfo{
		BinaryPath:     filepath.Join(binDir, binaryName),
		VersionCommand: pkg.VersionCommand,
		Mode:           "",
	}, nil
}

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

	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "", fmt.Errorf("binary not found: %s", binaryPath)
	}

	wasCustomCommand := versionCommand != ""
	if versionCommand == "" {
		versionCommand = "--version"
	}

	isShellCommand := ContainsShellOperators(versionCommand)
	binDir := filepath.Dir(binaryPath)
	if mode == "directory" {
		binDir = filepath.Dir(binaryPath)
	}

	cmdParts := strings.Fields(versionCommand)
	versionCommands := getVersionCommandVariations(wasCustomCommand, cmdParts)
	timeout := 10 * time.Second

	var output []byte
	var lastErr error

	for i, cmdArgs := range versionCommands {
		// Skip duplicate commands
		if i > 0 && isDuplicateCommand(cmdArgs, cmdParts) {
			continue
		}

		// Build process based on mode
		p, err := buildVersionCheckProcess(
			cmdArgs,
			binaryPath,
			binDir,
			mode,
			isShellCommand && wasCustomCommand && i == 0,
			versionCommand,
			t,
		)
		if err != nil {
			lastErr = err
			continue
		}

		// Run with timeout
		result := p.WithTimeout(timeout).Run()
		if result.Err != nil {
			lastErr = result.Err
			continue
		}

		output = []byte(result.Out())
		if len(output) > 0 {
			lastErr = nil
			break
		}
		lastErr = fmt.Errorf("no output from command")
	}

	if lastErr != nil {
		t.V(3).Infof("All version commands failed for %s", utils.LogPath(binaryPath))
		return "", fmt.Errorf("all version commands failed, last error: %v", lastErr)
	}

	version, err := parseVersionOutput(string(output), versionPattern)
	if err != nil {
		return "", err
	}

	t.V(3).Infof("Successfully extracted version: %s", version)
	return version, nil
}

// CheckBinaryVersion checks the version of a binary against expected versions
func CheckBinaryVersion(t *task.Task, tool string, pkg types.Package, binDir string, expectedVersion, requestedVersion string) types.CheckResult {
	result := types.CheckResult{
		Tool:             tool,
		ExpectedVersion:  expectedVersion,
		RequestedVersion: requestedVersion,
	}

	pathInfo, err := resolveBinaryPath(tool, pkg, binDir, "")
	if err != nil {
		result.Status = types.CheckStatusError
		result.Error = err.Error()
		return result
	}

	result.BinaryPath = pathInfo.BinaryPath

	// Check if binary/directory exists
	if stat, err := os.Stat(pathInfo.BinaryPath); os.IsNotExist(err) {
		result.Status = types.CheckStatusMissing
		if pathInfo.Mode == "directory" {
			result.Error = fmt.Sprintf("Package directory not found: %s", pathInfo.BinaryPath)
		} else {
			result.Error = fmt.Sprintf("Binary not found: %s", pathInfo.BinaryPath)
		}
		t.V(3).Infof("Binary/directory not found: %s", utils.LogPath(pathInfo.BinaryPath))
		return result
	} else if pathInfo.Mode == "directory" && !stat.IsDir() {
		result.Status = types.CheckStatusMissing
		result.Error = fmt.Sprintf("Package directory not found: %s", pathInfo.BinaryPath)
		t.V(3).Infof("Package directory not found: %s", utils.LogPath(pathInfo.BinaryPath))
		return result
	}

	// Get installed version
	installedVersion, err := GetInstalledVersionWithMode(t, pathInfo.BinaryPath, pathInfo.VersionCommand, pkg.VersionPattern, pathInfo.Mode)
	if err != nil {
		result.Status = types.CheckStatusError
		result.Error = fmt.Sprintf("Failed to get version: %v", err)
		t.V(3).Infof("Failed to get version for %s: %v", tool, err)
		return result
	}

	result.InstalledVersion = installedVersion
	t.V(3).Infof("Found installed version %s for %s", installedVersion, tool)

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

	status, err := compareVersions(installedVersion, compareVersion)
	if err != nil {
		result.Status = types.CheckStatusError
		result.Error = err.Error()
		return result
	}

	result.Status = status

	// Log comparison result
	switch status {
	case types.CheckStatusOK:
		t.V(3).Infof("Version match: %s == %s", installedVersion, compareVersion)
	case types.CheckStatusNewer:
		t.V(3).Infof("Newer version installed: %s > %s", installedVersion, compareVersion)
	case types.CheckStatusOutdated:
		t.V(3).Infof("Outdated version: %s < %s", installedVersion, compareVersion)
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
		name = strings.TrimSuffix(name, ".exe")

		tools = append(tools, name)
	}

	return tools, nil
}

// CheckExistingInstallation checks if a binary exists and matches the requested version
// Returns the existing version if it matches, empty string otherwise
func CheckExistingInstallation(t *task.Task, name string, pkg types.Package, requestedVersion, binDir string, osOverride string) string {
	t.V(3).Infof("Checking existing installation of %s (requested: %s)", name, requestedVersion)

	pathInfo, err := resolveBinaryPath(name, pkg, binDir, osOverride)
	if err != nil {
		t.V(1).Infof(err.Error())
		return ""
	}

	// Check if binary/directory exists
	if pathInfo.Mode == "directory" {
		if stat, err := os.Stat(pathInfo.BinaryPath); os.IsNotExist(err) || !stat.IsDir() {
			t.V(1).Infof("%s does not exist", pathInfo.BinaryPath)
			return ""
		}
	} else {
		if _, err := os.Stat(pathInfo.BinaryPath); os.IsNotExist(err) {
			t.V(1).Infof("%s does not exist", pathInfo.BinaryPath)
			return ""
		}
	}

	// Try to get the installed version
	installedVersion, err := GetInstalledVersionWithMode(t, pathInfo.BinaryPath, pathInfo.VersionCommand, pkg.VersionPattern, pathInfo.Mode)
	if err != nil {
		t.V(2).Infof(err.Error())
		return ""
	}

	// Normalize both versions for comparison
	normalizedInstalled := Normalize(installedVersion)
	normalizedRequested := Normalize(requestedVersion)

	if normalizedInstalled == normalizedRequested {
		t.V(3).Infof("Existing installation matches requested version: %s", installedVersion)
		return installedVersion
	}

	t.V(3).Infof("Existing installation version mismatch: %s != %s", installedVersion, requestedVersion)
	return ""
}
