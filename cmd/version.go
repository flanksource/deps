package cmd

import (
	"fmt"
	"os"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
)

type VersionOptions struct {
	Latest   bool     `json:"latest,omitempty"`
	Checksum bool     `json:"checksum,omitempty"`
	Tools    []string `json:"tools,omitempty" arg:"positional"`
}

type DependencyVersion struct {
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
	Status   string `json:"status"`
	Checksum string `json:"checksum,omitempty"`
}

func init() {
	clicky.AddCommand(rootCmd, VersionOptions{}, func(opts VersionOptions) (any, error) {
		return GetVersion(opts, opts.Tools...)
	})
}

func GetVersion(opts VersionOptions, dependencies ...string) ([]DependencyVersion, error) {
	var results []DependencyVersion
	var err error

	task.StartTask("version-check", func(ctx flanksourceContext.Context, t *task.Task) (interface{}, error) {
		results, err = getVersionWithTask(opts, dependencies, t)
		return results, err
	})

	return results, err
}

func getVersionWithTask(opts VersionOptions, dependencies []string, t *task.Task) ([]DependencyVersion, error) {
	// Load global configuration (defaults + user)
	depsConfig := config.GetGlobalRegistry()

	// Load lock file if it exists (optional)
	lockFile, err := config.LoadLockFile("")
	if err != nil {
		t.V(3).Infof("No lock file found: %v", err)
	}

	var toolsToCheck []string

	// If specific tools are requested
	if len(dependencies) > 0 {
		t.V(3).Infof("Checking specific tools: %v", dependencies)
		toolsToCheck = dependencies
	} else {
		// Check all tools in bin directory
		if _, err := os.Stat(binDir); os.IsNotExist(err) {
			return nil, fmt.Errorf("bin directory %s does not exist", binDir)
		}

		t.V(3).Infof("Scanning bin directory: %s", binDir)

		// List all files in bin directory
		entries, err := os.ReadDir(binDir)
		if err != nil {
			return nil, fmt.Errorf("failed to read bin directory: %w", err)
		}

		t.V(3).Infof("Found %d entries in bin directory", len(entries))

		for _, entry := range entries {
			name := entry.Name()

			// Skip hidden files
			if name[0] == '.' {
				continue
			}

			// Check if this is a known dependency
			if config.PackageExists(name) {
				t.V(3).Infof("Adding known tool: %s", name)
				toolsToCheck = append(toolsToCheck, name)
			} else {
				t.V(4).Infof("Skipping unknown tool: %s", name)
			}
		}

		t.V(3).Infof("Found %d known tools to check", len(toolsToCheck))

		if len(toolsToCheck) == 0 {
			return []DependencyVersion{}, nil
		}
	}

	var results []DependencyVersion
	for _, toolName := range toolsToCheck {
		t.V(3).Infof("Checking version for: %s", toolName)
		result := checkToolVersion(toolName, opts, t, depsConfig, lockFile)
		results = append(results, result)
	}

	t.V(3).Infof("Returning %d results", len(results))
	return results, nil
}

func checkToolVersion(toolName string, opts VersionOptions, t *task.Task, depsConfig *types.DepsConfig, lockFile *types.LockFile) DependencyVersion {
	pkg, exists := config.GetPackage(toolName)
	if !exists {
		return DependencyVersion{
			Name:   toolName,
			Status: "unknown",
		}
	}

	// Get version metadata from lock file if available
	if lockFile != nil {
		if lockEntry, exists := lockFile.Dependencies[toolName]; exists {
			if lockEntry.VersionCommand != "" && pkg.VersionCommand == "" {
				pkg.VersionCommand = lockEntry.VersionCommand
			}
			if lockEntry.VersionRegex != "" && pkg.VersionRegex == "" {
				pkg.VersionRegex = lockEntry.VersionRegex
			}
		}
	}

	// Check binary version
	result := version.CheckBinaryVersion(t, toolName, pkg, binDir, "", "")

	status := ""
	installedVersion := result.InstalledVersion
	checksumValue := ""

	switch result.Status {
	case types.CheckStatusMissing:
		status = "missing"
	case types.CheckStatusError:
		status = "error"
		installedVersion = result.Error
	case types.CheckStatusOK, types.CheckStatusNewer, types.CheckStatusOutdated, types.CheckStatusUnknown:
		status = "installed"
	default:
		status = "unknown"
	}

	// Add checksum if requested
	if opts.Checksum && result.ActualChecksum != "" {
		checksumValue = result.ActualChecksum
	}

	return DependencyVersion{
		Name:     toolName,
		Version:  installedVersion,
		Status:   status,
		Checksum: checksumValue,
	}
}
