package cmd

import (
	"fmt"
	"os"

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/types"
	"github.com/spf13/cobra"
)

var (
	initFromExisting bool
	initExample      bool
	initForce        bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new deps.yaml configuration file",
	Long: `Initialize a new deps.yaml configuration file.

This command creates a new deps.yaml file with either:
- An example configuration with common dependencies
- Migrated configuration from the existing hardcoded registry
- A minimal configuration template

Examples:
  # Create example configuration
  deps init

  # Create configuration from existing registry
  deps init --from-existing

  # Create example configuration with common tools
  deps init --example

  # Overwrite existing configuration
  deps init --force
`,
	RunE: initRun,
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().BoolVar(&initFromExisting, "from-existing", false, "Migrate from existing hardcoded dependencies")
	initCmd.Flags().BoolVar(&initExample, "example", false, "Create example configuration with common tools")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite existing deps.yaml")
}

func initRun(cmd *cobra.Command, args []string) error {
	// Check if deps.yaml already exists
	if _, err := os.Stat(config.DepsFile); err == nil && !initForce {
		return fmt.Errorf("deps.yaml already exists (use --force to overwrite)")
	}

	var depsConfig *types.DepsConfig

	// Create minimal configuration
	fmt.Println("Creating minimal configuration...")
	depsConfig = config.CreateDefaultConfig()

	// Add a few common tools as examples
	depsConfig.Dependencies = map[string]string{
		"kubectl": "latest",
	}

	depsConfig.Registry = map[string]types.Package{
		"kubectl": {
			Name:           "kubectl",
			Manager:        "direct",
			URLTemplate:    "https://storage.googleapis.com/kubernetes-release/release/{{.version}}/bin/{{.os}}/{{.arch}}/kubectl",
			VersionCommand: "version --client",
			VersionRegex:   `Client Version:\s*v?(\d+\.\d+\.\d+)`,
		},
	}

	fmt.Printf("✓ Created minimal configuration\n")

	// Save the configuration
	if err := config.SaveDepsConfig(depsConfig, ""); err != nil {
		return fmt.Errorf("failed to save deps.yaml: %w", err)
	}

	fmt.Printf("✓ Created deps.yaml\n")

	// Print next steps
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Edit deps.yaml to add your dependencies")
	fmt.Println("  2. Run 'deps lock' to generate deps-lock.yaml")
	fmt.Println("  3. Run 'deps install' to install dependencies")

	// If we migrated, suggest running lock
	if initFromExisting {
		fmt.Println("\nRecommended: Run 'deps lock --all' to generate multi-platform lock file")
	}

	return nil
}
