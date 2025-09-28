package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/verify"
	"github.com/flanksource/deps/pkg/version"
	"github.com/spf13/cobra"
)

var (
	checkAll     bool
	checkVerbose bool
	checkUpdate  bool
	checkVerify  bool
)

var checkCmd = &cobra.Command{
	Use:   "check [tool...]",
	Short: "Check versions of installed tools",
	Long: `Check the versions of installed tools in the bin directory.

Compares installed versions against:
- Expected versions from deps-lock.yaml (if exists)
- Requested versions from deps.yaml

When --verify is used, also verifies checksums:
- From deps-lock.yaml platform entries (if exists)
- Downloaded from source using checksum discovery strategies

Examples:
  deps check                    # Check all installed tools
  deps check kubectl helm      # Check specific tools
  deps check --verbose         # Show detailed information
  deps check --all             # Force check all configured tools
  deps check --verify          # Check versions and verify checksums
  deps check --verify kubectl  # Verify specific tool checksum`,
	RunE: runCheck,
}

func init() {
	rootCmd.AddCommand(checkCmd)

	checkCmd.Flags().BoolVar(&checkAll, "all", false, "Check all configured tools, not just installed ones")
	checkCmd.Flags().BoolVar(&checkVerbose, "verbose", false, "Show verbose output including commands and errors")
	checkCmd.Flags().BoolVar(&checkUpdate, "update", false, "Update outdated tools (not implemented yet)")
	checkCmd.Flags().BoolVar(&checkVerify, "verify", false, "Verify checksums of installed binaries against expected values")
}

func runCheck(cmd *cobra.Command, args []string) error {
	var checkErr error
	task.StartTask("check-dependencies", func(ctx flanksourceContext.Context, task *task.Task) (interface{}, error) {
		checkErr = runCheckWithTask(args, task)
		return nil, checkErr
	})
	return checkErr
}

func runCheckWithTask(args []string, t *task.Task) error {
	// Load global configuration (defaults + user)
	depsConfig := config.GetGlobalRegistry()

	// Load lock file if it exists
	var lockFile *types.LockFile
	var err error
	if lockFile, err = config.LoadLockFile(""); err != nil {
		// Lock file is optional
		if checkVerbose {
			fmt.Printf("No lock file found: %v\n\n", err)
		}
	}

	binDir := depsConfig.Settings.BinDir
	if binDir == "" {
		binDir = "./bin"
	}

	// Ensure bin directory exists
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		return fmt.Errorf("bin directory does not exist: %s", binDir)
	}

	// Determine which tools to check
	var toolsToCheck []string

	if len(args) > 0 {
		// Check specific tools provided as arguments
		toolsToCheck = args
	} else if checkAll {
		// Check all configured tools
		for tool := range depsConfig.Registry {
			toolsToCheck = append(toolsToCheck, tool)
		}
	} else {
		// Check installed tools only
		installedTools, err := version.ScanBinDirectory(binDir)
		if err != nil {
			return fmt.Errorf("failed to scan bin directory: %w", err)
		}

		// Filter to only include tools that are in our registry
		for _, tool := range installedTools {
			if _, exists := depsConfig.Registry[tool]; exists {
				toolsToCheck = append(toolsToCheck, tool)
			}
		}
	}

	if len(toolsToCheck) == 0 {
		fmt.Println("No tools to check. Use 'deps install' to install tools or 'deps check --all' to check all configured tools.")
		return nil
	}

	sort.Strings(toolsToCheck)

	// Check each tool
	var results []types.CheckResult
	var summary types.CheckSummary

	for _, tool := range toolsToCheck {
		pkg, exists := depsConfig.Registry[tool]
		if !exists {
			result := types.CheckResult{
				Tool:   tool,
				Status: types.CheckStatusError,
				Error:  "Tool not found in registry",
			}
			results = append(results, result)
			summary.AddResult(result)
			continue
		}

		// Get expected version from lock file
		var expectedVersion, requestedVersion string
		if lockFile != nil {
			if lockEntry, exists := lockFile.Dependencies[tool]; exists {
				expectedVersion = lockEntry.Version
			}
		}

		// Get requested version from deps.yaml
		if constraint, exists := depsConfig.Dependencies[tool]; exists {
			requestedVersion = constraint
		}

		if checkVerbose {
			fmt.Printf("Checking %s...\n", tool)
			if pkg.VersionCommand != "" {
				fmt.Printf("  Version command: %s\n", pkg.VersionCommand)
			}
			if pkg.VersionPattern != "" {
				fmt.Printf("  Version pattern: %s\n", pkg.VersionPattern)
			}
		}

		result := version.CheckBinaryVersion(t, tool, pkg, binDir, expectedVersion, requestedVersion)

		// Perform checksum verification if requested and binary is available
		if checkVerify && result.Status != types.CheckStatusMissing && result.Status != types.CheckStatusError {
			checksumResult := verify.VerifyBinaryChecksum(tool, pkg, binDir, lockFile, depsConfig.Settings.Platform)
			result.ChecksumStatus = checksumResult.ChecksumStatus
			result.ExpectedChecksum = checksumResult.ExpectedChecksum
			result.ActualChecksum = checksumResult.ActualChecksum
			result.ChecksumType = checksumResult.ChecksumType
			result.ChecksumError = checksumResult.ChecksumError
			result.ChecksumSource = checksumResult.ChecksumSource

			if checkVerbose && checksumResult.ChecksumError != "" {
				fmt.Printf("  Checksum error: %s\n", checksumResult.ChecksumError)
			}
		} else if checkVerify {
			result.ChecksumStatus = types.ChecksumStatusSkipped
		}

		results = append(results, result)
		summary.AddResult(result)

		if checkVerbose && result.Error != "" {
			fmt.Printf("  Error: %s\n", result.Error)
		}
	}

	// Display results
	displayCheckResults(results, summary)

	// Exit with error code if there are serious issues (errors and missing tools)
	if summary.Errors > 0 || summary.Missing > 0 {
		return fmt.Errorf("found %d errors and %d missing tools", summary.Errors, summary.Missing)
	}

	// Provide helpful messages for different states
	if summary.Outdated > 0 {
		fmt.Printf("\n💡 Run 'deps install' to update outdated tools\n")
	}
	if summary.Newer > 0 {
		fmt.Printf("\n⬆️ %d tools have newer versions than expected (usually OK)\n", summary.Newer)
	}

	return nil
}

func displayCheckResults(results []types.CheckResult, summary types.CheckSummary) {
	hasChecksums := false
	for _, result := range results {
		if result.ChecksumStatus != "" {
			hasChecksums = true
			break
		}
	}

	if hasChecksums {
		fmt.Println("Tool Version and Checksum Check Results:")
	} else {
		fmt.Println("Tool Version Check Results:")
	}
	fmt.Println()

	// Create table writer
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Print header
	if hasChecksums {
		fmt.Fprintln(w, "Tool\tInstalled\tExpected\tStatus\tChecksum")
		fmt.Fprintln(w, "────\t─────────\t────────\t──────\t────────")
	} else {
		fmt.Fprintln(w, "Tool\tInstalled\tExpected\tStatus")
		fmt.Fprintln(w, "────\t─────────\t────────\t──────")
	}

	// Print results
	for _, result := range results {
		status := formatStatus(result.Status)
		installed := result.InstalledVersion
		expected := result.ExpectedVersion

		// Use requested version if no expected version
		if expected == "" && result.RequestedVersion != "" {
			expected = result.RequestedVersion
		}

		// Handle missing/error cases
		if result.Status == types.CheckStatusMissing {
			installed = "❌ missing"
		} else if result.Status == types.CheckStatusError {
			installed = "❌ error"
		} else if installed == "" {
			installed = "❓ unknown"
		}

		if expected == "" {
			expected = "-"
		}

		if hasChecksums {
			checksumStatus := verify.FormatChecksumStatus(result.ChecksumStatus)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", result.Tool, installed, expected, status, checksumStatus)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", result.Tool, installed, expected, status)
		}
	}

	w.Flush()

	// Print summary
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Total: %d\n", summary.Total)
	if summary.OK > 0 {
		fmt.Printf("  ✅ OK: %d\n", summary.OK)
	}
	if summary.Outdated > 0 {
		fmt.Printf("  ⚠️  Outdated: %d\n", summary.Outdated)
	}
	if summary.Newer > 0 {
		fmt.Printf("  ⬆️ Newer: %d\n", summary.Newer)
	}
	if summary.Missing > 0 {
		fmt.Printf("  ❌ Missing: %d\n", summary.Missing)
	}
	if summary.Errors > 0 {
		fmt.Printf("  🚫 Errors: %d\n", summary.Errors)
	}
	if summary.Unknown > 0 {
		fmt.Printf("  ❓ Unknown: %d\n", summary.Unknown)
	}

	// Print checksum summary if verification was performed
	if hasChecksums {
		fmt.Printf("\nChecksum Verification:\n")
		if summary.ChecksumVerified > 0 {
			fmt.Printf("  ✅ Verified: %d\n", summary.ChecksumVerified)
		}
		if summary.ChecksumMismatch > 0 {
			fmt.Printf("  ❌ Mismatch: %d\n", summary.ChecksumMismatch)
		}
		if summary.ChecksumError > 0 {
			fmt.Printf("  🚫 Error: %d\n", summary.ChecksumError)
		}
		if summary.ChecksumSkipped > 0 {
			fmt.Printf("  ⏭️  Skipped: %d\n", summary.ChecksumSkipped)
		}
	}
}

func formatStatus(status types.CheckStatus) string {
	switch status {
	case types.CheckStatusOK:
		return "✅ OK"
	case types.CheckStatusOutdated:
		return "⚠️  OUTDATED"
	case types.CheckStatusNewer:
		return "⬆️ NEWER"
	case types.CheckStatusMissing:
		return "❌ MISSING"
	case types.CheckStatusError:
		return "🚫 ERROR"
	case types.CheckStatusUnknown:
		return "❓ UNKNOWN"
	default:
		return string(status)
	}
}
