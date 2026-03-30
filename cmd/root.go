package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	// Register all package managers via init functions
	_ "github.com/flanksource/deps/pkg/manager/apache"
	_ "github.com/flanksource/deps/pkg/manager/direct"
	_ "github.com/flanksource/deps/pkg/manager/github"
	_ "github.com/flanksource/deps/pkg/manager/gitlab"
	_ "github.com/flanksource/deps/pkg/manager/golang"
	_ "github.com/flanksource/deps/pkg/manager/maven"
	_ "github.com/flanksource/deps/pkg/manager/url"
)

var (
	binDir         string
	appDir         string
	tmpDir         string
	cacheDir       string
	force          bool
	skipChecksum   bool
	strictChecksum bool
	verbose        bool
	debug          bool
	osOverride     string
	archOverride   string
	configFile     string
	depsConfig     *types.DepsConfig
	versionInfo    VersionInfo
	showVersion    bool
	systemInstall  bool
	timeout        time.Duration
)

var clickyFlagNames = map[string]struct{}{
	"graceful-timeout": {},
	"json-logs":        {},
	"log-level":        {},
	"log-to-stderr":    {},
	"loglevel":         {},
	"max-concurrent":   {},
	"max-retries":      {},
	"no-progress":      {},
	"report-caller":    {},
	"retry-delay":      {},
}

type VersionInfo struct {
	Version string
	Commit  string
	Date    string
	Dirty   string
}

func SetVersion(version, commit, date, dirty string) {
	versionInfo = VersionInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
		Dirty:   dirty,
	}
}

func GetVersionInfo() VersionInfo {
	return versionInfo
}

var rootCmd = &cobra.Command{
	Use:   "deps",
	Short: "A dependency manager for downloading and installing binary tools",
	Long: `deps is a CLI tool for managing binary dependencies.
It can download and install various tools like kubectl, helm, terraform, and more.`,
	SilenceUsage: true,
	Run: func(cmd *cobra.Command, args []string) {
		// Handle --version flag when no subcommand is specified
		if showVersion {
			printVersion()
			return
		}
		// Show help if no version flag and no subcommand
		_ = cmd.Help()
	},
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Handle --version flag for subcommands
		if showVersion {
			printVersion()
			os.Exit(0)
		}

		if systemInstall {
			binDir = "/usr/local/bin"
			appDir = "/usr/local"
		}

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

		logger.Debugf("Using BIN_DIR: %s (%s/%s)", binDir, osOverride, archOverride)
	},
}

func printVersion() {
	dirtyStr := ""
	if versionInfo.Dirty == "true" {
		dirtyStr = " (dirty)"
	}
	fmt.Printf("deps version %s\n", versionInfo.Version)
	fmt.Printf("  commit: %s%s\n", versionInfo.Commit, dirtyStr)
	fmt.Printf("  built: %s\n", versionInfo.Date)
	fmt.Printf("  platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
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
	properties.BindFlags(rootCmd.PersistentFlags())
	rootCmd.SetUsageFunc(groupedUsageFunc)
	home := "/usr/local"
	if os.Geteuid() != 0 {
		if userHome, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(userHome, ".local")
		}
	}

	defaultAppDir := filepath.Join(home, "opt")
	defaultBinDir := filepath.Join(home, "bin")
	if d := os.Getenv("APP_DIR"); d != "" {
		defaultAppDir = d
	}
	if d := os.Getenv("BIN_DIR"); d != "" {
		defaultBinDir = d
	}

	rootCmd.PersistentFlags().BoolVar(&showVersion, "version", false, "Show version information")
	rootCmd.PersistentFlags().StringVar(&binDir, "bin-dir", defaultBinDir, "Directory to install binaries")
	rootCmd.PersistentFlags().StringVar(&appDir, "app-dir", defaultAppDir, "Directory to install directory-mode packages")
	rootCmd.PersistentFlags().StringVar(&tmpDir, "tmp-dir", os.TempDir(), "Directory for temporary files (will not be cleaned up on exit)")
	rootCmd.PersistentFlags().StringVar(&cacheDir, "cache-dir", "", "Directory for download cache (default: ~/.deps/cache, empty to disable)")
	rootCmd.PersistentFlags().BoolVar(&force, "force", false, "Force reinstall even if binary exists")
	rootCmd.PersistentFlags().BoolVar(&skipChecksum, "skip-checksum", false, "Skip checksum verification")
	rootCmd.PersistentFlags().BoolVar(&strictChecksum, "strict-checksum", true, "Fail installation when checksum verification fails (default: true)")
	rootCmd.PersistentFlags().StringVar(&osOverride, "os", runtime.GOOS, "Target OS (linux, darwin, windows)")
	rootCmd.PersistentFlags().StringVar(&archOverride, "arch", runtime.GOARCH, "Target architecture (amd64, arm64, etc.)")
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Path to deps.yaml config file")
	rootCmd.PersistentFlags().BoolVar(&systemInstall, "system", false, "Install system-wide (--bin-dir /usr/local/bin --app-dir /usr/local)")
	rootCmd.PersistentFlags().DurationVar(&timeout, "timeout", 5*time.Minute, "Timeout for downloads and installations")
}

func groupedUsageFunc(cmd *cobra.Command) error {
	out := cmd.OutOrStderr()

	fmt.Fprintf(out, "Usage:\n  %s\n", cmd.UseLine())

	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out, "\nAvailable Commands:")
		for _, sub := range cmd.Commands() {
			if !sub.IsAvailableCommand() || sub.Hidden {
				continue
			}
			fmt.Fprintf(out, "  %-12s %s\n", sub.Name(), sub.Short)
		}
	}

	if flags := filteredFlagUsages(cmd.Flags(), func(flag *pflag.Flag) bool {
		return !isClickyFlag(flag)
	}); flags != "" {
		fmt.Fprintln(out, "\nFlags:")
		fmt.Fprintln(out, flags)
	}

	if flags := filteredFlagUsages(cmd.Flags(), isClickyFlag); flags != "" {
		fmt.Fprintln(out, "\nClicky Flags:")
		fmt.Fprintln(out, flags)
	}

	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(out, "\nUse %q for more information about a command.\n", cmd.CommandPath()+" [command] --help")
	}

	return nil
}

func filteredFlagUsages(flags *pflag.FlagSet, include func(*pflag.Flag) bool) string {
	filtered := pflag.NewFlagSet(flags.Name(), pflag.ContinueOnError)
	filtered.SortFlags = true

	flags.VisitAll(func(flag *pflag.Flag) {
		if !include(flag) {
			return
		}
		cloned := *flag
		filtered.AddFlag(&cloned)
	})

	return strings.TrimRight(filtered.FlagUsagesWrapped(80), "\n")
}

func isClickyFlag(flag *pflag.Flag) bool {
	_, ok := clickyFlagNames[flag.Name]
	return ok
}
