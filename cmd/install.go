package cmd

import (
	"fmt"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/installer"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/verify"
	"github.com/flanksource/deps/pkg/version"
	"github.com/spf13/cobra"
)

var (
	installCheck bool
)

var installCmd = &cobra.Command{
	Use:          "install [tool[@version]...]",
	Short:        "Install one or more dependencies",
	SilenceUsage: true,
	Long: `Install one or more dependencies with optional version specification.

If no arguments are provided, installs all dependencies from deps.yaml.

Examples:
  deps install                       # Install all dependencies from deps.yaml
  deps install jq                    # Install jq with default version
  deps install kubectl@v1.28.0       # Install kubectl version v1.28.0
  deps install jq yq@v4.16.2 kind    # Install multiple tools
  deps install --check jq            # Install jq and verify the installation`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().BoolVar(&installCheck, "check", false, "Verify installation by checking version after install")
}

func runInstall(cmd *cobra.Command, args []string) error {
	// Create installer with options from global flags and depsConfig
	inst := installer.NewWithConfig(
		GetDepsConfig(),
		installer.WithBinDir(binDir),
		installer.WithTmpDir(tmpDir),
		installer.WithForce(force),
		installer.WithSkipChecksum(skipChecksum),
		installer.WithStrictChecksum(strictChecksum),
		installer.WithDebug(debug),
		installer.WithOS(osOverride, archOverride),
	)

	// If no arguments provided, install from deps.yaml
	if len(args) == 0 {
		var installErr error
		task.StartTask("install-from-config", func(ctx flanksourceContext.Context, task *task.Task) (interface{}, error) {
			installErr = inst.InstallFromConfig(task)
			return nil, installErr
		})
		if installErr != nil {
			return installErr
		}
	} else {
		// Parse tools and install them
		tools := installer.ParseTools(args)
		if err := inst.InstallMultiple(tools); err != nil {
			return err
		}
	}

	// Wait for all installations to complete
	exitCode := clicky.WaitForGlobalCompletion()
	if exitCode != 0 {
		return fmt.Errorf("installation failed with exit code %d", exitCode)
	}

	// Perform post-install check if requested
	if installCheck {
		fmt.Println("\n🔍 Verifying installations...")
		if err := runPostInstallCheck(args); err != nil {
			fmt.Printf("⚠️  Installation verification failed: %v\n", err)
			// Don't return error as installation succeeded, just verification failed
		}
	}

	return nil
}

// runPostInstallCheck performs version checks on installed tools
func runPostInstallCheck(args []string) error {
	// Use global depsConfig
	depsConfig := GetDepsConfig()
	if depsConfig == nil {
		return fmt.Errorf("configuration not loaded")
	}

	binDir := depsConfig.Settings.BinDir
	if binDir == "" {
		binDir = "./bin"
	}

	// Determine which tools to check
	var toolsToCheck []string
	if len(args) == 0 {
		// If installing from config, check all configured tools
		for tool := range depsConfig.Registry {
			toolsToCheck = append(toolsToCheck, tool)
		}
	} else {
		// Check only the tools that were installed
		tools := installer.ParseTools(args)
		for _, tool := range tools {
			toolsToCheck = append(toolsToCheck, tool.Name)
		}
	}

	// Load lock file for checksum verification
	var lockFile *types.LockFile
	var lockErr error
	if lockFile, lockErr = config.LoadLockFile(""); lockErr != nil {
		// Lock file is optional, continue without it
		lockFile = nil
	}

	// Check each tool
	var results []types.CheckResult
	var hasErrors bool
	var checksumIssues bool

	for _, tool := range toolsToCheck {
		pkg, exists := depsConfig.Registry[tool]
		if !exists {
			continue
		}

		// Get requested version from deps.yaml
		var requestedVersion string
		if constraint, exists := depsConfig.Dependencies[tool]; exists {
			requestedVersion = constraint
		}

		result := version.CheckBinaryVersion(tool, pkg, binDir, "", requestedVersion)

		// Perform checksum verification
		if result.Status != types.CheckStatusMissing && result.Status != types.CheckStatusError {
			checksumResult := verify.VerifyBinaryChecksum(tool, pkg, binDir, lockFile, depsConfig.Settings.Platform)
			result.ChecksumStatus = checksumResult.ChecksumStatus
			result.ExpectedChecksum = checksumResult.ExpectedChecksum
			result.ActualChecksum = checksumResult.ActualChecksum
			result.ChecksumType = checksumResult.ChecksumType
			result.ChecksumError = checksumResult.ChecksumError
			result.ChecksumSource = checksumResult.ChecksumSource
		}

		results = append(results, result)

		// Show results with both version and checksum status
		versionOK := result.Status == types.CheckStatusOK

		if result.Status == types.CheckStatusError || result.Status == types.CheckStatusMissing {
			hasErrors = true
			status := formatCheckStatus(result.Status)
			fmt.Printf("  %s: %s\n", tool, status)
			if result.Error != "" {
				fmt.Printf("    Error: %s\n", result.Error)
			}
		} else if result.ChecksumStatus == types.ChecksumStatusMismatch || result.ChecksumStatus == types.ChecksumStatusError {
			checksumIssues = true
			checksumStatus := verify.FormatChecksumStatus(result.ChecksumStatus)
			if versionOK {
				fmt.Printf("  %s: ✅ OK (%s) | Checksum: %s\n", tool, result.InstalledVersion, checksumStatus)
			} else {
				fmt.Printf("  %s: %s (%s) | Checksum: %s\n", tool, formatCheckStatus(result.Status), result.InstalledVersion, checksumStatus)
			}
			if result.ChecksumError != "" {
				fmt.Printf("    Checksum error: %s\n", result.ChecksumError)
			}
		} else if result.Status == types.CheckStatusNewer {
			checksumInfo := ""
			if result.ChecksumStatus == types.ChecksumStatusOK {
				checksumInfo = " | Checksum: ✅ VERIFIED"
			} else if result.ChecksumStatus != types.ChecksumStatusSkipped && result.ChecksumStatus != "" {
				checksumInfo = fmt.Sprintf(" | Checksum: %s", verify.FormatChecksumStatus(result.ChecksumStatus))
			}
			fmt.Printf("  %s: ⬆️ NEWER (%s, expected %s)%s\n", tool, result.InstalledVersion, result.ExpectedVersion, checksumInfo)
		} else if result.Status == types.CheckStatusOutdated {
			checksumInfo := ""
			if result.ChecksumStatus == types.ChecksumStatusOK {
				checksumInfo = " | Checksum: ✅ VERIFIED"
			} else if result.ChecksumStatus != types.ChecksumStatusSkipped && result.ChecksumStatus != "" {
				checksumInfo = fmt.Sprintf(" | Checksum: %s", verify.FormatChecksumStatus(result.ChecksumStatus))
			}
			fmt.Printf("  %s: ⚠️ OUTDATED (%s, expected %s)%s\n", tool, result.InstalledVersion, result.ExpectedVersion, checksumInfo)
		} else {
			checksumInfo := ""
			if result.ChecksumStatus == types.ChecksumStatusOK {
				checksumInfo = " | Checksum: ✅ VERIFIED"
			} else if result.ChecksumStatus != types.ChecksumStatusSkipped && result.ChecksumStatus != "" {
				checksumInfo = fmt.Sprintf(" | Checksum: %s", verify.FormatChecksumStatus(result.ChecksumStatus))
			}
			fmt.Printf("  %s: ✅ OK (%s)%s\n", tool, result.InstalledVersion, checksumInfo)
		}
	}

	if hasErrors {
		fmt.Println("\n💡 Run 'deps check --verbose' for detailed diagnostics")
	} else if checksumIssues {
		fmt.Println("\n⚠️ Installations have checksum verification issues!")
		fmt.Println("💡 Run 'deps check --verify --verbose' for detailed checksum diagnostics")
	} else {
		fmt.Println("✅ All installations verified successfully!")
	}

	return nil
}

func formatCheckStatus(status types.CheckStatus) string {
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
