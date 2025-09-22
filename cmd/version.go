package cmd

import (
	"fmt"
	"os"

	"github.com/flanksource/deps/pkg/config"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version [tool...]",
	Short: "Check installed versions of tools",
	Long: `Check the installed versions of specified tools.

If no tools are specified, checks all tools found in the bin directory.

Examples:
  deps version                # Check all installed tools
  deps version jq kubectl     # Check specific tools`,
	RunE: runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersion(cmd *cobra.Command, args []string) error {
	// If specific tools are requested
	if len(args) > 0 {
		for _, toolName := range args {
			checkToolVersion(toolName)
		}
		return nil
	}

	// Check all tools in bin directory
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		fmt.Printf("Bin directory %s does not exist\n", binDir)
		return nil
	}

	fmt.Printf("Checking tools in %s:\n\n", binDir)

	// List all files in bin directory
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return fmt.Errorf("failed to read bin directory: %w", err)
	}

	toolsFound := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Check if this is a known dependency
		if config.PackageExists(name) {
			checkToolVersion(name)
			toolsFound = true
		}
	}

	if !toolsFound {
		fmt.Println("No known tools found in bin directory")
	}

	return nil
}

func checkToolVersion(toolName string) {
	_, exists := config.GetPackage(toolName)
	if !exists {
		fmt.Printf("%-20s Unknown tool\n", toolName)
		return
	}

	// TODO: Implement version checking for new package system
	fmt.Printf("%-20s Version checking not yet implemented\n", toolName)
}
