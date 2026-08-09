package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/flanksource/deps"
)

// deps-start lives in its own module so the deps binary does not carry the
// docker and kubernetes SDKs; `deps svc` forwards to it instead.
const depsStartPackage = "deps-start"

var svcCmd = &cobra.Command{
	Use:   "svc",
	Short: "Start and manage services (postgres, opensearch, valkey, ...)",
	Long: `Start and manage services from the deps registry.

Every subcommand forwards to deps-start, which is installed on demand into the
deps bin directory. Service flags come from the registry, so they pass through
unchanged:

  deps svc start postgres@17
  deps svc logs postgres -f
  deps svc stop postgres

Starting leaves the service running in the background and exits once it is
ready; deps svc start --foreground runs it in this terminal instead.

Run deps-start directly for the full help of an individual service.`,
}

// svcVerbs are the deps-start subcommands exposed under `deps svc`.
var svcVerbs = map[string]string{
	"start":   "Start a service",
	"stop":    "Stop started services",
	"restart": "Restart services with the options they were started with",
	"status":  "Show the status of started services",
	"list":    "List started services",
	"logs":    "Show service logs",
	"info":    "Show live service configuration and connection details",
}

func init() {
	rootCmd.AddCommand(svcCmd)
	for verb, short := range svcVerbs {
		svcCmd.AddCommand(newSvcPassthroughCmd(verb, short))
	}
}

// newSvcPassthroughCmd forwards its arguments verbatim to deps-start. Flag
// parsing is disabled because every service contributes its own flags from
// its registry spec; redeclaring them here would fork the catalog.
func newSvcPassthroughCmd(verb, short string) *cobra.Command {
	return &cobra.Command{
		Use:                verb,
		Short:              short,
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		SilenceUsage:       true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsStart(cmd.Context(), append([]string{verb}, args...)...)
		},
	}
}

// runDepsStart runs deps-start with stdio inherited so its output and exit
// code reach the user unchanged.
func runDepsStart(ctx context.Context, args ...string) error {
	binary, err := resolveDepsStart(ctx)
	if err != nil {
		return err
	}
	child := osexec.CommandContext(ctx, binary, args...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Run(); err != nil {
		var exit *osexec.ExitError
		if errors.As(err, &exit) {
			// deps-start already reported the failure on stderr
			os.Exit(exit.ExitCode())
		}
		return fmt.Errorf("failed to run %s: %w", binary, err)
	}
	return nil
}

// resolveDepsStart returns the deps-start binary, preferring one already on
// PATH or in the deps bin directory and installing it from this repo's
// releases otherwise.
func resolveDepsStart(ctx context.Context) (string, error) {
	name := depsStartPackage
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if path, err := osexec.LookPath(name); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	binDir := filepath.Join(home, ".deps", "bin")
	installed := filepath.Join(binDir, name)
	if _, err := os.Stat(installed); err == nil {
		return installed, nil
	}
	if _, err := deps.InstallWithContext(ctx, depsStartPackage, "latest", deps.WithBinDir(binDir)); err != nil {
		return "", fmt.Errorf("failed to install %s: %w", depsStartPackage, err)
	}
	if _, err := os.Stat(installed); err != nil {
		return "", fmt.Errorf("%s was installed but not found at %s", depsStartPackage, installed)
	}
	return installed, nil
}
