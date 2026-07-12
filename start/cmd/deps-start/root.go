package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/flanksource/deps/start"
)

type rootFlags struct {
	runtime     string
	version     string
	port        int
	namespace   string
	dataDir     string
	stateDir    string
	detach      bool
	waitTimeout time.Duration
	output      string
}

func newRootCmd() *cobra.Command {
	flags := &rootFlags{}
	cmd := &cobra.Command{
		Use:     "deps-start <service>",
		Short:   "Start services (postgres, opensearch, valkey, ...) via binary, docker or helm",
		Long:    "deps-start launches services from the deps registry and prints a commons-db connection.\n\nAvailable services: " + strings.Join(start.ServiceNames(), ", "),
		Version: fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd, args[0], flags)
		},
		SilenceUsage: true,
	}
	cmd.Flags().StringVar(&flags.runtime, "runtime", "", "runtime to use: binary, docker or helm (default: first supported)")
	cmd.Flags().StringVar(&flags.version, "version-of", "", "service version to install/run (default: latest)")
	cmd.Flags().IntVar(&flags.port, "port", 0, "host port override for the primary service port")
	cmd.Flags().StringVarP(&flags.namespace, "namespace", "n", "default", "kubernetes namespace (helm runtime)")
	cmd.Flags().StringVar(&flags.dataDir, "data-dir", "", "service data directory override")
	cmd.PersistentFlags().StringVar(&flags.stateDir, "state-dir", "", "state directory (default ~/.deps/services)")
	cmd.Flags().BoolVarP(&flags.detach, "detach", "d", false, "run the service in the background")
	cmd.Flags().DurationVar(&flags.waitTimeout, "wait-timeout", 2*time.Minute, "readiness wait timeout")
	cmd.Flags().StringVarP(&flags.output, "output", "o", "yaml", "connection output format: yaml, json or env")

	cmd.AddCommand(newStopCmd(flags), newStatusCmd(flags), newListCmd(flags), newLogsCmd(flags))
	return cmd
}

func (f *rootFlags) options() []start.Option {
	var opts []start.Option
	if f.runtime != "" {
		opts = append(opts, start.WithRuntime(start.RuntimeKind(f.runtime)))
	}
	if f.version != "" {
		opts = append(opts, start.WithVersion(f.version))
	}
	if f.port != 0 {
		opts = append(opts, start.WithPort(f.port))
	}
	if f.dataDir != "" {
		opts = append(opts, start.WithDataDir(f.dataDir))
	}
	if f.stateDir != "" {
		opts = append(opts, start.WithStateDir(f.stateDir))
	}
	opts = append(opts,
		start.WithNamespace(f.namespace),
		start.WithDetach(f.detach),
		start.WithWaitTimeout(f.waitTimeout),
	)
	return opts
}

func runStart(cmd *cobra.Command, name string, flags *rootFlags) error {
	if flags.detach && os.Getenv(supervisorEnv) == "" {
		if err := runDetached(name, flags); err != nil {
			return err
		}
		instance, err := start.Get(cmd.Context(), name, flags.options()...)
		if err != nil {
			return err
		}
		return printConnection(instance, flags.output)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	instance, err := start.Start(ctx, name, flags.options()...)
	if err != nil {
		return err
	}
	if err := printConnection(instance, flags.output); err != nil {
		return err
	}
	if err := instance.Wait(ctx); err != nil && ctx.Err() != nil {
		return nil // interrupted: the service was stopped cleanly
	} else if err != nil {
		return err
	}
	return nil
}

func printConnection(instance *start.Instance, format string) error {
	conn := instance.Connection
	switch format {
	case "yaml":
		// round-trip through json so omitempty tags drop zero-value fields
		data, err := json.Marshal(conn)
		if err != nil {
			return err
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return err
		}
		delete(m, "id")
		for k, v := range m {
			if s, ok := v.(string); ok && (s == "" || strings.HasPrefix(s, "0001-01-01T")) {
				delete(m, k)
			}
		}
		return yaml.NewEncoder(os.Stdout).Encode(m)
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(conn)
	case "env":
		fmt.Printf("URL=%s\n", conn.URL)
		if conn.Username != "" {
			fmt.Printf("USERNAME=%s\n", conn.Username)
		}
		if conn.Password != "" {
			fmt.Printf("PASSWORD=%s\n", conn.Password)
		}
		for k, v := range conn.Properties {
			fmt.Printf("%s=%s\n", strings.ToUpper(k), v)
		}
		return nil
	default:
		return fmt.Errorf("unknown output format %q (yaml, json, env)", format)
	}
}
