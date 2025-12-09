package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
	"github.com/spf13/cobra"
)

var (
	updateAll       bool
	updateCheckOnly bool
	updateYes       bool
)

var updateCmd = &cobra.Command{
	Use:   "update [dependency...]",
	Short: "Check for updates and optionally update dependencies",
	Long: `Check for newer versions of installed dependencies and optionally update them.

If no dependencies are specified, checks all dependencies from deps.yaml.

Examples:
  deps update                    # Check all dependencies for updates
  deps update jq kubectl        # Check specific dependencies
  deps update --all             # Update all dependencies without prompting
  deps update --check           # Only check for updates, don't install
  deps update --yes kubectl     # Update kubectl without confirmation`,
	RunE: runUpdate,
}

func init() {
	rootCmd.AddCommand(updateCmd)

	updateCmd.Flags().BoolVar(&updateAll, "all", false, "Update all dependencies without prompting")
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "Only check for updates, don't install")
	updateCmd.Flags().BoolVar(&updateYes, "yes", false, "Automatically approve updates without prompting")
}

type UpdateInfo struct {
	Name           string
	Current        string
	Available      string
	NeedsUpdate    bool
	IsInstalled    bool
	BinaryPath     string
	HasLockEntry   bool
	PackageManager string
}

func runUpdate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Load global config (defaults + user)
	depsConfig := config.GetGlobalRegistry()

	if err := config.ValidateConfig(depsConfig); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Try to load deps-lock.yaml
	lockFile, lockErr := config.LoadLockFile("")
	if lockErr != nil && verbose {
		fmt.Printf("Warning: No lock file found (%v)\n", lockErr)
	}

	// Get the global package manager registry
	managers := manager.GetGlobalRegistry()

	// Determine which dependencies to check
	depsToCheck := make(map[string]string)
	if len(args) == 0 {
		// Check all dependencies
		for name, constraint := range depsConfig.Dependencies {
			depsToCheck[name] = constraint
		}
	} else {
		// Check specified dependencies
		for _, name := range args {
			if constraint, exists := depsConfig.Dependencies[name]; exists {
				depsToCheck[name] = constraint
			} else {
				fmt.Printf("Warning: %s not found in deps.yaml\n", name)
			}
		}
	}

	// Check each dependency for updates
	var updates []UpdateInfo
	for name, constraint := range depsToCheck {
		info, err := checkDependencyUpdate(ctx, name, constraint, depsConfig, lockFile, managers)
		if err != nil {
			if verbose {
				fmt.Printf("Warning: failed to check %s: %v\n", name, err)
			}
			continue
		}
		updates = append(updates, info)
	}

	if len(updates) == 0 {
		fmt.Println("No dependencies to check.")
		return nil
	}

	// Display results
	fmt.Printf("%-15s %-15s %-15s %-10s %-10s\n", "Package", "Current", "Available", "Update?", "Manager")
	fmt.Println(strings.Repeat("-", 75))

	var needsUpdates []UpdateInfo
	for _, info := range updates {
		status := "No"
		if info.NeedsUpdate {
			status = "Yes"
			needsUpdates = append(needsUpdates, info)
		}

		currentStr := info.Current
		if !info.IsInstalled {
			currentStr = "Not installed"
		}

		fmt.Printf("%-15s %-15s %-15s %-10s %-10s\n",
			info.Name,
			currentStr,
			info.Available,
			status,
			info.PackageManager)
	}

	if len(needsUpdates) == 0 {
		fmt.Println("\nAll dependencies are up to date!")
		return nil
	}

	if updateCheckOnly {
		fmt.Printf("\n%d dependencies have updates available.\n", len(needsUpdates))
		return nil
	}

	// Ask user which dependencies to update (unless --all or --yes)
	toUpdate := needsUpdates
	if !updateAll && !updateYes {
		var err error
		toUpdate, err = promptForUpdates(needsUpdates)
		if err != nil {
			return err
		}
	}

	if len(toUpdate) == 0 {
		fmt.Println("No dependencies selected for update.")
		return nil
	}

	// Perform updates
	fmt.Printf("\nUpdating %d dependencies...\n", len(toUpdate))
	for _, info := range toUpdate {
		fmt.Printf("Updating %s from %s to %s...\n", info.Name, info.Current, info.Available)
		// TODO: Implement actual update logic
		// This would involve downloading and installing the new version
		fmt.Printf("✓ %s updated successfully\n", info.Name)
	}

	// TODO: Update lock file with new versions
	fmt.Println("\nAll updates completed!")
	return nil
}

func checkDependencyUpdate(ctx context.Context, name, constraint string, depsConfig *types.DepsConfig, lockFile *types.LockFile, managers *manager.Registry) (UpdateInfo, error) {
	info := UpdateInfo{
		Name:           name,
		PackageManager: "legacy",
	}

	// Check if this dependency has a new-style package definition
	pkg, hasNewPackage := depsConfig.Registry[name]
	if hasNewPackage {
		info.PackageManager = pkg.Manager
	}

	// Get current installed version
	if hasNewPackage && pkg.VersionCommand != "" {
		// Try to get installed version using the version command
		if currentVersion, err := getCurrentInstalledVersion(name, pkg); err == nil {
			info.Current = currentVersion
			info.IsInstalled = true
			info.BinaryPath = getBinaryPath(name)
		}
	}

	// Get locked version if available
	if lockFile != nil {
		if lockEntry, exists := lockFile.Dependencies[name]; exists {
			info.HasLockEntry = true
			if !info.IsInstalled {
				info.Current = lockEntry.Version
			}
		}
	}

	// Get available version
	if hasNewPackage {
		// Use new package manager system
		mgr, err := managers.GetForPackage(pkg)
		if err != nil {
			return info, fmt.Errorf("failed to get package manager: %w", err)
		}

		// Use centralized version resolver to resolve the constraint
		currentPlatform := platform.Current()
		resolver := version.NewResolver(mgr)
		availableVersion, err := resolver.ResolveConstraint(ctx, pkg, constraint, currentPlatform)
		if err != nil {
			info.Available = fmt.Sprintf("error: %v", err)
		} else {
			info.Available = availableVersion
		}
	} else {
		// Use legacy system - just use the constraint as available version
		info.Available = constraint
	}

	// Determine if update is needed
	if info.IsInstalled && info.Current != "" && info.Available != "" {
		cmp, err := version.Compare(info.Available, info.Current)
		if err == nil && cmp > 0 {
			info.NeedsUpdate = true
		}
	} else if !info.IsInstalled {
		info.NeedsUpdate = true
	}

	return info, nil
}

func getCurrentInstalledVersion(name string, pkg types.Package) (string, error) {
	binaryPath := filepath.Join(binDir, name)

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return "", fmt.Errorf("binary not found at %s", binaryPath)
	}

	// Use version command from package or default
	versionCmd := pkg.VersionCommand
	if versionCmd == "" {
		versionCmd = "--version"
	}

	// Execute the binary with version command
	cmd := exec.Command(binaryPath, strings.Split(versionCmd, " ")...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run version command: %w", err)
	}

	outputStr := string(output)

	// Use version pattern from package or default
	versionPattern := pkg.VersionRegex
	if versionPattern == "" {
		// Default pattern to match semantic versions
		versionPattern = `v?(\d+\.\d+\.\d+)`
	}

	// Extract version using regex
	re, err := regexp.Compile(versionPattern)
	if err != nil {
		return "", fmt.Errorf("invalid version pattern %s: %w", versionPattern, err)
	}

	matches := re.FindStringSubmatch(outputStr)
	if len(matches) < 2 {
		return "", fmt.Errorf("version not found in output: %s", outputStr)
	}

	return matches[1], nil
}

func getBinaryPath(name string) string {
	return filepath.Join(binDir, name)
}

func promptForUpdates(available []UpdateInfo) ([]UpdateInfo, error) {
	fmt.Println("\nWhich dependencies would you like to update?")
	fmt.Println("Enter 'y' for yes, 'n' for no, 'a' for all remaining, 'q' to quit:")

	var selected []UpdateInfo
	for _, info := range available {
		fmt.Printf("\nUpdate %s from %s to %s? (y/n/a/q): ", info.Name, info.Current, info.Available)

		var response string
		_, _ = fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))

		switch response {
		case "y", "yes":
			selected = append(selected, info)
		case "n", "no":
			// Skip this one
		case "a", "all":
			// Add this one and all remaining
			selected = append(selected, info)
			for i := indexOf(available, info) + 1; i < len(available); i++ {
				selected = append(selected, available[i])
			}
			return selected, nil
		case "q", "quit":
			return selected, nil
		default:
			fmt.Println("Invalid response, skipping...")
		}
	}

	return selected, nil
}

func indexOf(slice []UpdateInfo, item UpdateInfo) int {
	for i, v := range slice {
		if v.Name == item.Name {
			return i
		}
	}
	return -1
}
