package main

import (
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/flanksource/deps/start"
	"github.com/flanksource/deps/start/state"
)

// completeServiceNames offers registry service names for shell completion.
func completeServiceNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return start.ServiceNames(), cobra.ShellCompDirectiveNoFileComp
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
				fmt.Fprintf(os.Stderr, "stopped %s\n", name)
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
// service and relaunches it detached with its persisted start options.
func restartService(cmd *cobra.Command, name string, flags *rootFlags) error {
	instance, err := start.Restart(cmd.Context(), name, flags.options()...)
	if err == nil {
		fmt.Fprintf(os.Stderr, "restarted %s in place\n", name)
		return printConnection(instance, "yaml")
	}
	if !errors.Is(err, start.ErrRestartUnsupported) && !errors.Is(err, start.ErrNotRunning) {
		return err
	}

	// no in-place restart: stop if running, then relaunch detached with the
	// persisted options so a binary supervisor outlives this command
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
		fmt.Fprintf(os.Stderr, "stopped %s\n", name)
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
	args := []string{name, "--detach"}
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
	return args
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

func printStatusTable(cmd *cobra.Command, flags *rootFlags, instances []*start.Instance) error {
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tRUNTIME\tSTATUS\tVERSION\tPORTS\tCPU\tMEM\tUPTIME\tURL")
	for _, i := range instances {
		status, err := start.Status(cmd.Context(), i.Name, flags.options()...)
		if err != nil {
			status = state.StatusUnknown
		}
		display := string(status)
		ports, cpu, mem := "", "", ""
		if status == state.StatusRunning {
			if res := i.Metrics(cmd.Context()); res != nil {
				ports = joinPorts(res.Ports)
				cpu = fmt.Sprintf("%.1f%%", res.CPUPercent)
				mem = humanBytes(res.RSSBytes)
				if res.Restarts > 0 {
					display += fmt.Sprintf(" (%d restarts)", res.Restarts)
				}
			}
		}
		if ports == "" {
			ports = joinPorts(portValues(i.State.Ports))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			i.Name, i.Runtime, display, i.State.Version,
			ports, cpu, mem, uptime(i.State.StartedAt, status), i.Connection.String())
	}
	return w.Flush()
}

func joinPorts(ports []int) string {
	strs := make([]string, len(ports))
	for i, p := range ports {
		strs[i] = strconv.Itoa(p)
	}
	return strings.Join(strs, ",")
}

func portValues(named map[string]int) []int {
	var ports []int
	for _, p := range named {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

func humanBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0fMiB", float64(b)/(1<<20))
	case b > 0:
		return fmt.Sprintf("%.0fKiB", float64(b)/(1<<10))
	default:
		return ""
	}
}

func uptime(started time.Time, status state.Status) string {
	if started.IsZero() || status != state.StatusRunning {
		return ""
	}
	return time.Since(started).Round(time.Second).String()
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
