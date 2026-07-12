package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/flanksource/deps/start"
)

func newStopCmd(flags *rootFlags) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "stop [service...]",
		Short: "Stop started services",
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
		Use:   "status [service]",
		Short: "Show the status of started services",
		Args:  cobra.MaximumNArgs(1),
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
	fmt.Fprintln(w, "NAME\tRUNTIME\tSTATUS\tVERSION\tSTARTED\tURL")
	for _, i := range instances {
		status, err := start.Status(cmd.Context(), i.Name, flags.options()...)
		if err != nil {
			status = "unknown"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			i.Name, i.Runtime, status, i.State.Version,
			i.State.StartedAt.Format(time.RFC3339), i.Connection.String())
	}
	return w.Flush()
}

func newLogsCmd(flags *rootFlags) *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <service>",
		Short: "Show service logs",
		Args:  cobra.ExactArgs(1),
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
