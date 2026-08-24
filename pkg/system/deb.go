package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// InstallDeb installs a Debian package.
//
// This exists for packages that only work at the absolute prefix they were
// built for — an omnibus build links /opt/<project> into its interpreter, its
// shared libraries and its load path, so unpacking one into bin-dir produces a
// tree that cannot run. dpkg puts it where the package expects to be.
func InstallDeb(debPath, destDir string, opts *SystemInstallOptions) (*SystemInstallResult, error) {
	result := &SystemInstallResult{
		RequiredSudo: true,
		SystemWide:   true,
		ToolName:     opts.ToolName,
	}

	if runtime.GOOS != "linux" {
		return result, fmt.Errorf(".deb files can only be installed on Linux")
	}
	if _, err := exec.LookPath("dpkg"); err != nil {
		return result, fmt.Errorf("dpkg is not available: cannot install %s", filepath.Base(debPath))
	}

	if !opts.Silent {
		displaySystemWideWarning(opts.ToolName, debPath)
		if !promptForConfirmation("Continue? [y/N]: ") {
			return result, fmt.Errorf("installation cancelled by user")
		}
	}

	if opts.Task != nil {
		opts.Task.Infof("🔐 Installing %s system-wide...", opts.ToolName)
		opts.Task.Infof("   Please enter your password when prompted")
	}

	var cmd *exec.Cmd
	if opts.SkipSudo {
		// Also the path taken when deps already runs as root, which is the
		// normal case in a container.
		cmd = exec.Command("dpkg", "--install", debPath)
	} else {
		cmd = exec.Command("sudo", "dpkg", "--install", debPath)
	}

	// Connected for the interactive password prompt.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return result, fmt.Errorf("failed to install .deb: %w", err)
	}

	toolName := opts.ToolName
	if toolName == "" {
		// Debian names artifacts <package>_<version>-<revision>_<arch>.deb, so
		// everything from the first underscore is version metadata.
		toolName = strings.TrimSuffix(filepath.Base(debPath), ".deb")
		if idx := strings.Index(toolName, "_"); idx > 0 {
			toolName = toolName[:idx]
		}
	}

	binaryPath, err := findInstalledBinary(toolName, opts.Task)
	if err != nil {
		if opts.Task != nil {
			opts.Task.Infof("⚠️ Installation completed but binary not found in PATH: %v", err)
		}
		result.InstallPath = "/usr/bin"
	} else {
		result.BinaryPath = binaryPath
		result.InstallPath = filepath.Dir(binaryPath)
	}

	return result, nil
}
