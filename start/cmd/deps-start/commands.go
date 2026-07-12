package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
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
			instance, err := start.Get(cmd.Context(), args[0], flags.options()...)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("service %s has not been started", args[0])
				}
				return err
			}
			if instance.State.LogFile == "" {
				return fmt.Errorf("service %s (%s runtime) has no log file", args[0], instance.Runtime)
			}
			return printLogs(cmd.Context(), instance.State.LogFile, follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	return cmd
}

func printLogs(ctx interface{ Done() <-chan struct{} }, path string, follow bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(os.Stdout, f); err != nil {
		return err
	}
	for follow {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
		if _, err := io.Copy(os.Stdout, f); err != nil {
			return err
		}
	}
	return nil
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
