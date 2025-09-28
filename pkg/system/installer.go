package system

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/flanksource/clicky/task"
)

// SystemInstallResult contains information about the system installation result
type SystemInstallResult struct {
	BinaryPath   string // Path to the installed binary
	InstallPath  string // Directory where installation occurred
	RequiredSudo bool   // Whether sudo was required
	SystemWide   bool   // Whether this was a system-wide installation
	ToolName     string // Name of the tool that was installed
}

// SystemInstallOptions contains options for system installation
type SystemInstallOptions struct {
	ToolName string // Name of the tool being installed
	Silent   bool   // Skip user confirmation prompts
	SkipSudo bool   // Skip sudo requirements (for testing)
	Task     *task.Task
}

// GetSystemInstallerType returns the type of system installer
func GetSystemInstallerType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".pkg":
		return "macos_installer"
	case ".msi":
		return "windows_installer"
	default:
		return "unknown"
	}
}

// InstallSystemPackage handles installation of system installers
func InstallSystemPackage(installerPath, destDir string, opts *SystemInstallOptions) (*SystemInstallResult, error) {
	if opts == nil {
		opts = &SystemInstallOptions{}
	}

	installerType := GetSystemInstallerType(installerPath)

	switch installerType {
	case "macos_installer":
		return InstallPkg(installerPath, destDir, opts)
	case "windows_installer":
		return InstallMsi(installerPath, destDir, opts)
	default:
		return nil, fmt.Errorf("unsupported installer type: %s", filepath.Ext(installerPath))
	}
}

// InstallPkg installs a macOS .pkg file
func InstallPkg(pkgPath, destDir string, opts *SystemInstallOptions) (*SystemInstallResult, error) {
	result := &SystemInstallResult{
		RequiredSudo: true,
		SystemWide:   true,
		ToolName:     opts.ToolName,
	}

	// Validate platform
	if runtime.GOOS != "darwin" {
		return result, fmt.Errorf(".pkg files can only be installed on macOS")
	}

	// Display warning and get user confirmation
	if !opts.Silent {
		displaySystemWideWarning(opts.ToolName, pkgPath)
		if !promptForConfirmation("Continue? [y/N]: ") {
			return result, fmt.Errorf("installation cancelled by user")
		}
	}

	if opts.Task != nil {
		opts.Task.Infof("🔐 Installing %s system-wide...", opts.ToolName)
		opts.Task.Infof("   Please enter your password when prompted")
	}

	// Execute installer command
	var cmd *exec.Cmd
	if opts.SkipSudo {
		// For testing without sudo
		cmd = exec.Command("installer", "-pkg", pkgPath, "-target", "/")
	} else {
		cmd = exec.Command("sudo", "installer", "-pkg", pkgPath, "-target", "/")
	}

	// Connect stdin/stdout/stderr for interactive password prompt
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return result, fmt.Errorf("failed to install .pkg: %w", err)
	}

	// Try to find the installed binary
	toolName := opts.ToolName
	if toolName == "" {
		// Extract tool name from pkg filename
		toolName = strings.TrimSuffix(filepath.Base(pkgPath), filepath.Ext(pkgPath))
		// Remove version info (e.g., AWSCLIV2-2.15.0 -> AWSCLIV2)
		if idx := strings.Index(toolName, "-"); idx > 0 {
			toolName = toolName[:idx]
		}
	}

	binaryPath, err := findInstalledBinary(toolName, opts.Task)
	if err != nil {
		if opts.Task != nil {
			opts.Task.Infof("⚠️ Installation completed but binary not found in PATH: %v", err)
		}
		// Installation succeeded but we couldn't locate the binary
		result.InstallPath = "/Applications" // Common install location
	} else {
		result.BinaryPath = binaryPath
		result.InstallPath = filepath.Dir(binaryPath)
	}

	return result, nil
}

// InstallMsi installs a Windows .msi file
func InstallMsi(msiPath, destDir string, opts *SystemInstallOptions) (*SystemInstallResult, error) {
	result := &SystemInstallResult{
		RequiredSudo: true, // May require admin privileges
		SystemWide:   true,
		ToolName:     opts.ToolName,
	}

	// Validate platform
	if runtime.GOOS != "windows" {
		return result, fmt.Errorf(".msi files can only be installed on Windows")
	}

	// Display warning and get user confirmation
	if !opts.Silent {
		displaySystemWideWarning(opts.ToolName, msiPath)
		if !promptForConfirmation("Continue? [y/N]: ") {
			return result, fmt.Errorf("installation cancelled by user")
		}
	}

	if opts.Task != nil {
		opts.Task.Infof("🔐 Installing %s system-wide...", opts.ToolName)
		opts.Task.Infof("   Administrator privileges may be required")
	}

	// Execute msiexec command
	cmd := exec.Command("msiexec", "/i", msiPath, "/quiet", "/norestart")

	// Connect stdin/stdout/stderr for any interactive prompts
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return result, fmt.Errorf("failed to install .msi: %w", err)
	}

	// Try to find the installed binary
	toolName := opts.ToolName
	if toolName == "" {
		// Extract tool name from msi filename
		toolName = strings.TrimSuffix(filepath.Base(msiPath), filepath.Ext(msiPath))
		// Remove version info
		if idx := strings.Index(toolName, "-"); idx > 0 {
			toolName = toolName[:idx]
		}
	}

	binaryPath, err := findInstalledBinary(toolName, opts.Task)
	if err != nil {
		if opts.Task != nil {
			opts.Task.Infof("⚠️ Installation completed but binary not found in PATH: %v", err)
		}
		// Installation succeeded but we couldn't locate the binary
		result.InstallPath = "C:\\Program Files" // Common install location
	} else {
		result.BinaryPath = binaryPath
		result.InstallPath = filepath.Dir(binaryPath)
	}

	return result, nil
}

// findInstalledBinary searches for an installed binary in common system locations
func findInstalledBinary(toolName string, t *task.Task) (string, error) {
	// First check if binary is in PATH
	binaryName := toolName
	if runtime.GOOS == "windows" && !strings.HasSuffix(binaryName, ".exe") {
		binaryName += ".exe"
	}

	if path, err := exec.LookPath(binaryName); err == nil {
		if t != nil {
			t.Debugf("Found %s in PATH: %s", toolName, path)
		}
		return path, nil
	}

	// Platform-specific search paths
	var searchPaths []string

	switch runtime.GOOS {
	case "darwin":
		searchPaths = []string{
			"/usr/local/bin/" + binaryName,
			"/opt/homebrew/bin/" + binaryName,
			"/usr/bin/" + binaryName,
		}
		// Also check in Applications for tools that install there
		appPaths := []string{
			"/Applications/*/Contents/MacOS/" + binaryName,
			"/Applications/*/*/Contents/MacOS/" + binaryName,
		}
		for _, pattern := range appPaths {
			matches, _ := filepath.Glob(pattern)
			searchPaths = append(searchPaths, matches...)
		}

	case "windows":
		programFiles := os.Getenv("PROGRAMFILES")
		programFilesX86 := os.Getenv("PROGRAMFILES(X86)")
		localAppData := os.Getenv("LOCALAPPDATA")

		if programFiles != "" {
			searchPaths = append(searchPaths, filepath.Join(programFiles, "*", "bin", binaryName))
			searchPaths = append(searchPaths, filepath.Join(programFiles, "*", binaryName))
		}
		if programFilesX86 != "" {
			searchPaths = append(searchPaths, filepath.Join(programFilesX86, "*", "bin", binaryName))
			searchPaths = append(searchPaths, filepath.Join(programFilesX86, "*", binaryName))
		}
		if localAppData != "" {
			searchPaths = append(searchPaths, filepath.Join(localAppData, "Programs", "*", binaryName))
		}

	case "linux":
		searchPaths = []string{
			"/usr/local/bin/" + binaryName,
			"/usr/bin/" + binaryName,
			"/opt/*/bin/" + binaryName,
		}
	}

	// Search in specified paths
	for _, searchPath := range searchPaths {
		if strings.Contains(searchPath, "*") {
			// Handle glob patterns
			matches, _ := filepath.Glob(searchPath)
			for _, match := range matches {
				if fileExists(match) {
					if t != nil {
						t.Debugf("Found installed binary: %s", match)
					}
					return match, nil
				}
			}
		} else {
			// Direct path check
			if fileExists(searchPath) {
				if t != nil {
					t.Debugf("Found installed binary: %s", searchPath)
				}
				return searchPath, nil
			}
		}
	}

	return "", fmt.Errorf("binary not found after installation: %s", toolName)
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// promptForConfirmation asks the user for confirmation
func promptForConfirmation(message string) bool {
	fmt.Print(message)
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// displaySystemWideWarning shows a warning about system-wide installation
func displaySystemWideWarning(toolName, installerFile string) {
	fmt.Printf("\n⚠️  SYSTEM-WIDE INSTALLATION REQUIRED\n")
	fmt.Printf("   Tool: %s\n", toolName)
	fmt.Printf("   File: %s\n", filepath.Base(installerFile))
	fmt.Printf("   Action: Install system-wide using %s installer\n", runtime.GOOS)
	fmt.Printf("   Requires: Administrator privileges\n\n")
}
