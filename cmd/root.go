package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"github.com/spf13/cobra"

	// Register all package managers via init functions
	_ "github.com/flanksource/deps/pkg/manager/direct"
	_ "github.com/flanksource/deps/pkg/manager/github"
	_ "github.com/flanksource/deps/pkg/manager/gitlab"
	_ "github.com/flanksource/deps/pkg/manager/maven"
)

var (
	binDir         string
	tmpDir         string
	force          bool
	skipChecksum   bool
	strictChecksum bool
	verbose        bool
	debug          bool
	osOverride     string
	archOverride   string
	configFile     string
	depsConfig     *types.DepsConfig
)

var rootCmd = &cobra.Command{
	Use:   "deps",
	Short: "A dependency manager for downloading and installing binary tools",
	Long: `deps is a CLI tool for managing binary dependencies.
It can download and install various tools like kubectl, helm, terraform, and more.`,
	SilenceUsage: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Apply clicky flags after command line parsing
		clicky.Flags.UseFlags()

		// Set global platform overrides from CLI flags
		platform.SetGlobalOverrides(osOverride, archOverride)

		// Initialize global depsConfig
		var err error
		depsConfig, err = config.LoadMergedConfig(configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		logger.Infof("Installing to %s (%s/%s)", binDir, osOverride, archOverride)
	},
}

func Execute() error {
	return rootCmd.Execute()
}

// GetDepsConfig returns the global depsConfig
func GetDepsConfig() *types.DepsConfig {
	return depsConfig
}

func init() {

	clicky.BindAllFlags(rootCmd.PersistentFlags(), "tasks", "!format")

	home, err := os.UserHomeDir()
	if err != nil {
		home = "/usr/local"
	}
	defaultBinDir := fmt.Sprintf("%s/.local/bin", home)

	// Check if running as root/sudo
	if os.Geteuid() == 0 {
		defaultBinDir = "/usr/local/bin"
	}

	rootCmd.PersistentFlags().StringVar(&binDir, "bin-dir", defaultBinDir, "Directory to install binaries")
	rootCmd.PersistentFlags().StringVar(&tmpDir, "tmp-dir", os.TempDir(), "Directory for temporary files (will not be cleaned up on exit)")
	rootCmd.PersistentFlags().BoolVar(&force, "force", false, "Force reinstall even if binary exists")
	rootCmd.PersistentFlags().BoolVar(&skipChecksum, "skip-checksum", false, "Skip checksum verification")
	rootCmd.PersistentFlags().BoolVar(&strictChecksum, "strict-checksum", true, "Fail installation when checksum verification fails (default: true)")
	rootCmd.PersistentFlags().StringVar(&osOverride, "os", runtime.GOOS, "Target OS (linux, darwin, windows)")
	rootCmd.PersistentFlags().StringVar(&archOverride, "arch", runtime.GOARCH, "Target architecture (amd64, arm64, etc.)")
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Path to deps.yaml config file")
}
