package runtime

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/installer"
	"github.com/flanksource/deps/pkg/version"
)

// runtimeDetector handles runtime detection and version checking
type runtimeDetector struct {
	language       string
	binaryVariants []string
	versionCmd     []string // Command to get version (e.g., ["--version"] or ["-version"])
	versionRegex   *regexp.Regexp
	task           *task.Task
}

// detectRuntime finds the runtime binary and extracts its version
func (d *runtimeDetector) detectRuntime() (*runtimeInfo, error) {
	// Check cache first
	if info, exists := getCachedRuntime(d.language); exists {
		// Validate cached path still exists
		if _, err := os.Stat(info.Path); err == nil {
			if d.task != nil {
				d.task.V(4).Infof("Using cached %s runtime: %s version %s", d.language, info.Path, info.Version)
			}
			return info, nil
		}
		// Cached path no longer exists, invalidate and continue with PATH search
		_ = invalidateCache(d.language)
	}

	// Search PATH for runtime binary
	binaryPath, err := findBinaryInPath(d.binaryVariants...)
	if err != nil {
		return nil, fmt.Errorf("%s runtime not found in PATH (searched: %s)", d.language, strings.Join(d.binaryVariants, ", "))
	}

	if d.task != nil {
		d.task.V(4).Infof("Found %s binary at: %s", d.language, binaryPath)
	}

	// Get version
	runtimeVersion, err := d.getVersion(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s version: %w", d.language, err)
	}

	if d.task != nil {
		d.task.V(4).Infof("%s version: %s", d.language, runtimeVersion)
	}

	// Cache the result
	info := &runtimeInfo{
		Path:    binaryPath,
		Version: runtimeVersion,
	}

	if err := setCachedRuntime(d.language, info); err != nil {
		// Log but don't fail on cache errors
		if d.task != nil {
			d.task.V(5).Infof("Failed to cache runtime info: %v", err)
		}
	}

	return info, nil
}

// getVersion executes the version command and extracts the version string
func (d *runtimeDetector) getVersion(binaryPath string) (string, error) {
	// Build command: binaryPath + versionCmd args
	cmd := append([]string{binaryPath}, d.versionCmd...)

	// Execute command to get version
	process := clicky.Exec(cmd[0], cmd[1:]...)

	if d.task != nil {
		process = process.WithTask(d.task)
	}

	result := process.Run()

	if result.Err != nil {
		return "", result.Err
	}

	versionStr := d.parseVersion(result.Out())

	if versionStr == "" {
		return "", fmt.Errorf("failed to parse version from output: %s", result.Out())
	}

	return versionStr, nil
}

// parseVersion extracts version string using regex
func (d *runtimeDetector) parseVersion(output string) string {
	if d.versionRegex == nil {
		return ""
	}

	matches := d.versionRegex.FindStringSubmatch(output)
	if len(matches) < 2 {
		return ""
	}

	return strings.TrimSpace(matches[1])
}

// checkVersionConstraint verifies the runtime version meets the constraint
func (d *runtimeDetector) checkVersionConstraint(runtimeVersion, constraint string) (bool, error) {
	if constraint == "" {
		return true, nil // No constraint specified
	}

	// Parse constraint
	c, err := version.ParseConstraint(constraint)
	if err != nil {
		return false, fmt.Errorf("invalid version constraint: %w", err)
	}

	// Check if runtime version satisfies constraint
	matches := c.Check(runtimeVersion)

	if d.task != nil {
		if matches {
			d.task.V(4).Infof("%s version %s satisfies constraint %s", d.language, runtimeVersion, constraint)
		} else {
			d.task.V(4).Infof("%s version %s does NOT satisfy constraint %s", d.language, runtimeVersion, constraint)
		}
	}

	return matches, nil
}

// findOrInstallRuntime finds an existing runtime or installs it if needed
func (d *runtimeDetector) findOrInstallRuntime(constraint string) (*runtimeInfo, error) {
	// Try to detect existing runtime
	info, err := d.detectRuntime()
	if err == nil {
		// Runtime found, check version constraint
		if constraint != "" {
			matches, checkErr := d.checkVersionConstraint(info.Version, constraint)
			if checkErr != nil {
				return nil, checkErr
			}

			if !matches {
				// Version mismatch, need to install correct version
				if d.task != nil {
					d.task.V(3).Infof("%s version %s does not match constraint %s, will install", d.language, info.Version, constraint)
				}
				// Invalidate cache since we need different version
				_ = invalidateCache(d.language)

				return d.installRuntime(constraint)
			}
		}

		// Runtime found and version matches
		return info, nil
	}

	// Runtime not found, install it
	if d.task != nil {
		d.task.V(3).Infof("%s runtime not found, will install", d.language)
	}

	return d.installRuntime(constraint)
}

// installRuntime installs the runtime using deps
func (d *runtimeDetector) installRuntime(constraint string) (*runtimeInfo, error) {
	// Determine version to install
	versionToInstall := "stable"
	if constraint != "" {
		versionToInstall = constraint
	}

	if d.task != nil {
		d.task.V(3).Infof("Installing %s version %s via deps", d.language, versionToInstall)
	}

	// Get global config
	depsConfig := config.GetGlobalRegistry()

	// Create installer
	inst := installer.NewWithConfig(depsConfig)

	// Ensure we have a task for installation
	installTask := d.task
	if installTask == nil {
		installTask = &task.Task{}
	}

	// Install the runtime
	packageName := d.language
	_, err := inst.InstallWithResult(packageName, versionToInstall, installTask)
	if err != nil {
		return nil, fmt.Errorf("failed to install %s: %w", d.language, err)
	}

	if d.task != nil {
		d.task.V(3).Infof("Successfully installed %s version %s", d.language, versionToInstall)
	}

	// After installation, detect the newly installed runtime
	// Clear cache first to force re-detection
	_ = invalidateCache(d.language)

	info, err := d.detectRuntime()
	if err != nil {
		return nil, fmt.Errorf("runtime installed but detection failed: %w", err)
	}

	return info, nil
}
