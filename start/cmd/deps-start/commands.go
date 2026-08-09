package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/flanksource/clicky/api/icons"

	"github.com/flanksource/deps/start"
	"github.com/flanksource/deps/start/state"
)

// completeServiceNames offers registry service names for shell completion.
func completeServiceNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return start.ServiceNames(), cobra.ShellCompDirectiveNoFileComp
}

// newStartCmd documents the start verb and completes service names. Real
// invocations never reach its RunE: normalizeArgs routes them to the
// service's own command, which carries the flags from its registry spec.
func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "start <service>[@version]",
		Short:             "Start a service (same as `deps-start <service>`)",
		Long:              "Start a service.\n\nEach service takes flags derived from its registry spec; run\n`deps-start <service> --help` (or `deps-start start <service> --help`) to see them.\nService flags must follow the service name.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completeServiceNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown service %q, available: %s", args[0], strings.Join(start.ServiceNames(), ", "))
		},
		SilenceUsage: true,
	}
}

func newStopCmd(flags *rootFlags) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:               "stop [service...]",
		Short:             "Stop started services",
		ValidArgsFunction: completeServiceNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			names := args
			if all {
				instances, err := start.List(cmd.Context(), flags.options()...)
				if err != nil {
					return err
				}
				for _, i := range instances {
					names = append(names, i.Name)
				}
			}
			if len(names) == 0 {
				return fmt.Errorf("specify a service or --all")
			}
			for _, name := range names {
				if err := start.Stop(cmd.Context(), name, flags.options()...); err != nil {
					return err
				}
				printAction(icons.Stop, "muted", "stopped %s", name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "stop every started service")
	return cmd
}

func newRestartCmd(flags *rootFlags) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:               "restart [service...]",
		Short:             "Restart services (detached) with the options they were started with",
		ValidArgsFunction: completeServiceNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			names := args
			if all {
				instances, err := start.List(cmd.Context(), flags.options()...)
				if err != nil {
					return err
				}
				for _, i := range instances {
					names = append(names, i.Name)
				}
			}
			if len(names) == 0 {
				return fmt.Errorf("specify a service or --all")
			}
			for _, name := range names {
				if err := restartService(cmd, name, flags); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "restart every started service")
	return cmd
}

// restartService restarts in place where the runtime supports it (binary via
// SupervisedProcess, docker via the restart API); otherwise it stops the
// service and starts it again with its persisted start options.
func restartService(cmd *cobra.Command, name string, flags *rootFlags) error {
	// a restart is health-gated, so show the service's log and what the
	// readiness wait is blocked on while it comes back
	stream := newServiceStream(name)
	started := time.Now()
	streamCtx, stopStream := context.WithCancel(cmd.Context())
	defer func() {
		stopStream()
		stream.Close()
	}()
	// the supervisor writing the log usually lives in another process, so
	// follow the log file rather than relying on the in-process tee
	if existing, err := start.Get(cmd.Context(), name, flags.options()...); err == nil && existing.State.LogFile != "" {
		tailLogsUntil(streamCtx, stream.logWriter(), logTailFrom(existing.State.LogFile))
	}

	instance, err := start.Restart(cmd.Context(), name, append(flags.options(), stream.options()...)...)
	if err == nil {
		stopStream()
		printAction(icons.Reload, "text-green-600", "restarted %s in place", name)
		info, infoErr := instance.Info(cmd.Context())
		if infoErr != nil {
			return infoErr
		}
		printReady(name, info, time.Since(started))
		return writeServiceOutput(os.Stdout, info, "json")
	}
	stopStream()
	if !errors.Is(err, start.ErrRestartUnsupported) && !errors.Is(err, start.ErrNotRunning) {
		return err
	}

	// no in-place restart: stop if running, then start again with the
	// persisted options, which backgrounds a supervisor that outlives this
	// command
	instance, err = start.Get(cmd.Context(), name, flags.options()...)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service %s has not been started", name)
		}
		return err
	}
	if status, err := start.Status(cmd.Context(), name, flags.options()...); err == nil && status == state.StatusRunning {
		if err := start.Stop(cmd.Context(), name, flags.options()...); err != nil {
			return fmt.Errorf("failed to stop %s for restart: %w", name, err)
		}
		printAction(icons.Stop, "muted", "stopped %s", name)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	child := osexec.CommandContext(cmd.Context(), exe, restartArgs(name, instance.State.StartOptions, flags.stateDir)...)
	child.Stdout, child.Stderr, child.Stdin = os.Stdout, os.Stderr, nil
	return child.Run()
}

func restartArgs(name string, so *state.StartOptions, stateDir string) []string {
	args := []string{name}
	if stateDir != "" {
		args = append(args, "--state-dir", stateDir)
	}
	if so == nil {
		return args
	}
	if so.Runtime != "" {
		args = append(args, "--runtime", so.Runtime)
	}
	if so.Version != "" {
		args = append(args, "--version", so.Version)
	}
	if so.Port != 0 {
		args = append(args, "--port", strconv.Itoa(so.Port))
	}
	if so.Bind != "" {
		args = append(args, "--bind", so.Bind)
	}
	// only helm-capable subcommands define --namespace; the default adds nothing
	if so.Namespace != "" && so.Namespace != "default" {
		args = append(args, "--namespace", so.Namespace)
	}
	if so.DataDir != "" {
		args = append(args, "--data-dir", so.DataDir)
	}
	if so.VolumeMode != "" {
		args = append(args, "--volume-mode", so.VolumeMode)
	}
	parameterNames := make([]string, 0, len(so.Parameters))
	for name := range so.Parameters {
		parameterNames = append(parameterNames, name)
	}
	sort.Strings(parameterNames)
	for _, name := range parameterNames {
		args = append(args, "--"+name+"="+so.Parameters[name])
	}
	return args
}

func newInfoCmd(flags *rootFlags) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:               "info <service>",
		Short:             "Show live service configuration and connection details",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeServiceNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			instance, err := start.Get(cmd.Context(), args[0], flags.options()...)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("service %s has not been started", args[0])
				}
				return err
			}
			info, infoErr := instance.Info(cmd.Context())
			if infoErr != nil {
				return infoErr
			}
			return writeServiceOutput(os.Stdout, info, output)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "json", "service output format: json, yaml or env")
	return cmd
}

func newStatusCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:               "status [service]",
		Short:             "Show the status of started services",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeServiceNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			instances, err := start.List(cmd.Context(), flags.options()...)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				var filtered []*start.Instance
				for _, i := range instances {
					if i.Name == args[0] {
						filtered = append(filtered, i)
					}
				}
				if len(filtered) == 0 {
					return fmt.Errorf("service %s has not been started", args[0])
				}
				instances = filtered
			}
			return printStatusTable(cmd, flags, instances)
		},
	}
}

func newListCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List started services",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			instances, err := start.List(cmd.Context(), flags.options()...)
			if err != nil {
				return err
			}
			return printStatusTable(cmd, flags, instances)
		},
	}
}

func newLogsCmd(flags *rootFlags) *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:               "logs <service>",
		Short:             "Show service logs",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeServiceNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			instance, err := start.Get(ctx, args[0], flags.options()...)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("service %s has not been started", args[0])
				}
				return err
			}
			return instance.Logs(ctx, follow, os.Stdout)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	return cmd
}

// tailFile returns the last n lines of a file, best effort.
func tailFile(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
