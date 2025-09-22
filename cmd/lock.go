package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/lock"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/types"
	"github.com/spf13/cobra"
)

var (
	lockAll        bool
	lockPlatforms  []string
	lockParallel   bool
	lockVerifyOnly bool
	lockUpdateOnly bool
	lockOutputFile string
	lockForce      bool
)

var lockCmd = &cobra.Command{
	Use:   "lock [package...]",
	Short: "Generate or update deps-lock.yaml with resolved versions and checksums",
	Long: `Generate or update deps-lock.yaml with resolved versions and checksums.

The lock file ensures reproducible builds by pinning exact versions and checksums
for all dependencies across all platforms.

Examples:
  # Lock current platform only
  deps lock

  # Lock all common platforms
  deps lock --all

  # Lock specific platforms
  deps lock --platforms linux-amd64,darwin-arm64

  # Lock with parallel downloads (faster)
  deps lock --all --parallel

  # Update existing lock file
  deps lock --update-only

  # Add new platform to existing lock
  deps lock --update-only --os windows --arch amd64

  # Lock only specific packages
  deps lock yq helm kubectl

  # Update lock for single package
  deps lock --update-only yq
`,
	RunE: lockRun,
}

func init() {
	rootCmd.AddCommand(lockCmd)

	lockCmd.Flags().BoolVar(&lockAll, "all", false, "Lock all common platforms")
	lockCmd.Flags().StringSliceVar(&lockPlatforms, "platforms", nil, "Specific platforms to lock (comma-separated)")
	lockCmd.Flags().BoolVar(&lockParallel, "parallel", false, "Download checksums in parallel")
	lockCmd.Flags().BoolVar(&lockVerifyOnly, "verify-only", false, "Only verify, don't download files")
	lockCmd.Flags().BoolVar(&lockUpdateOnly, "update-only", false, "Only update existing entries")
	lockCmd.Flags().StringVar(&lockOutputFile, "output", "", "Output lock file path (default: deps-lock.yaml)")
	lockCmd.Flags().BoolVar(&lockForce, "force", false, "Force re-resolution of all dependencies, even exact versions")
}

func lockRun(cmd *cobra.Command, args []string) error {

	// Load configuration
	depsConfig := GetDepsConfig()
	if depsConfig == nil {
		return fmt.Errorf("failed to load configuration")
	}

	if err := config.ValidateConfig(depsConfig); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Get the global package manager registry
	managers := manager.GetGlobalRegistry()

	// Create lock generator
	generator := lock.NewGenerator(managers)

	// Check if user provided platform overrides via --os or --arch flags
	platforms := lockPlatforms
	osFlag := cmd.Root().PersistentFlags().Lookup("os")
	archFlag := cmd.Root().PersistentFlags().Lookup("arch")
	osChanged := osFlag != nil && osFlag.Changed
	archChanged := archFlag != nil && archFlag.Changed
	if len(platforms) == 0 && (osChanged || archChanged) {
		// User provided CLI platform overrides, use those instead of existing lock file platforms
		platforms = []string{depsConfig.Settings.Platform.String()}
	}

	// Determine lock options
	opts := types.LockOptions{
		All:        lockAll,
		Platforms:  platforms,
		Packages:   args, // Package names from command line arguments
		Parallel:   lockParallel,
		VerifyOnly: lockVerifyOnly,
		UpdateOnly: lockUpdateOnly,
		Force:      lockForce,
	}


	var lockFile *types.LockFile
	var cleanedLockFile *types.LockFile
	var lockErr error

	// Run the lock generation in a task
	task.StartTask("lock-generation", func(ctx flanksourceContext.Context, t *task.Task) (interface{}, error) {
		// Check if we should update existing lock file
		existingLock, err := config.LoadLockFile("")
		if opts.UpdateOnly || err == nil {
			if err != nil {
				// Create an empty lock file if none exists but --update-only was specified
				existingLock = &types.LockFile{
					Version:         "1.0",
					Generated:       time.Now(),
					CurrentPlatform: depsConfig.Settings.Platform,
					Dependencies:    make(map[string]types.LockEntry),
				}
			}

			lockFile, lockErr = generator.Update(ctx.Context, existingLock, depsConfig.Dependencies, depsConfig.Registry, opts, t)
		} else {
			// Generate new lock file
			lockFile, lockErr = generator.Generate(ctx.Context, depsConfig.Dependencies, depsConfig.Registry, opts, t)
		}

		if lockErr != nil {
			return nil, lockErr
		}

		// Return the lock file without saving yet - we'll save after all tasks complete
		return lockFile, nil
	})

	// Wait for all tasks to complete
	exitCode := clicky.WaitForGlobalCompletion()
	if exitCode != 0 {
		return fmt.Errorf("lock generation failed with exit code %d", exitCode)
	}

	if lockErr != nil {
		return lockErr
	}

	// Now that all tasks are complete, clean up dependencies that have no successful platforms
	cleanedLockFile = cleanupFailedDependencies(lockFile)

	// Save the lock file only with successful entries
	outputPath := lockOutputFile
	if outputPath == "" {
		outputPath = config.LockFile
	}

	if err := config.SaveLockFile(cleanedLockFile, outputPath); err != nil {
		return fmt.Errorf("failed to save lock file: %w", err)
	}

	// Use the cleaned lock file for summary
	lockFile = cleanedLockFile

	// Print summary
	platformCount := countPlatforms(lockFile)
	dependencyCount := len(lockFile.Dependencies)

	fmt.Printf("✓ Locked %d dependencies for %d platforms in %s\n",
		dependencyCount,
		platformCount,
		formatDuration(time.Since(lockFile.Generated)))

	// Print platform breakdown
	if verbose || lockAll {
		printLockSummary(lockFile)
	}

	return nil
}

// countPlatforms counts unique platforms across all dependencies
func countPlatforms(lockFile *types.LockFile) int {
	platforms := make(map[string]bool)
	for _, entry := range lockFile.Dependencies {
		for platform := range entry.Platforms {
			platforms[platform] = true
		}
	}
	return len(platforms)
}

// printLockSummary prints a detailed summary of the lock file
func printLockSummary(lockFile *types.LockFile) {
	fmt.Println("\nLock file summary:")
	fmt.Printf("Generated: %s\n", lockFile.Generated.Format(time.RFC3339))
	fmt.Printf("Current platform: %s\n", lockFile.CurrentPlatform)

	// Group platforms
	platforms := make(map[string]bool)
	for _, entry := range lockFile.Dependencies {
		for platform := range entry.Platforms {
			platforms[platform] = true
		}
	}

	if len(platforms) > 0 {
		fmt.Printf("Platforms: %s\n", strings.Join(mapKeys(platforms), ", "))
	}

	fmt.Printf("\nDependencies:\n")
	for name, entry := range lockFile.Dependencies {
		fmt.Printf("  %s@%s (%d platforms)\n", name, entry.Version, len(entry.Platforms))

		if verbose {
			for platform, platformEntry := range entry.Platforms {
				checksumInfo := ""
				if platformEntry.Checksum != "" {
					checksumInfo = fmt.Sprintf(" ✓ %s", platformEntry.Checksum[:12]+"...")
				}

				archiveInfo := ""
				if platformEntry.Archive {
					archiveInfo = " (archive)"
				}

				fmt.Printf("    %s%s%s\n", platform, archiveInfo, checksumInfo)
			}
		}
	}
}

// mapKeys returns the keys of a string->bool map as a slice
func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

// cleanupFailedDependencies removes dependencies that have no successful platform resolutions
func cleanupFailedDependencies(lockFile *types.LockFile) *types.LockFile {
	cleaned := &types.LockFile{
		Version:         lockFile.Version,
		Generated:       lockFile.Generated,
		CurrentPlatform: lockFile.CurrentPlatform,
		Dependencies:    make(map[string]types.LockEntry),
	}

	for name, entry := range lockFile.Dependencies {
		// Only keep dependencies that have at least one successful platform and a version
		if entry.Version != "" && len(entry.Platforms) > 0 {
			cleaned.Dependencies[name] = entry
		}
	}

	return cleaned
}
