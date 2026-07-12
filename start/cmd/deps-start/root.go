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

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/start"
)

type rootFlags struct {
	runtime     string
	version     string
	port        int
	bind        string
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
		Use:     "deps-start <service>[@version]",
		Short:   "Start services (postgres, opensearch, valkey, ...) via binary, docker, helm or a CLI",
		Long:    "deps-start launches services from the deps registry and prints a commons-db connection.\n\nVersions use the same syntax and constraint semantics as deps install:\n  deps-start postgres@17    deps-start nats@2.11    deps-start valkey@8.1",
		Version: fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown service %q, available: %s", args[0], strings.Join(start.ServiceNames(), ", "))
		},
		SilenceUsage: true,
	}
	cmd.PersistentFlags().StringVar(&flags.stateDir, "state-dir", "", "state directory (default ~/.deps/services)")

	cmd.AddGroup(
		&cobra.Group{ID: "services", Title: "Services:"},
		&cobra.Group{ID: "management", Title: "Management:"},
	)
	for _, name := range start.ServiceNames() {
		cmd.AddCommand(newServiceCmd(name, flags))
	}
	for _, sub := range []*cobra.Command{newStopCmd(flags), newRestartCmd(flags), newStatusCmd(flags), newListCmd(flags), newLogsCmd(flags)} {
		sub.GroupID = "management"
		cmd.AddCommand(sub)
	}
	return cmd
}

// newServiceCmd builds the per-service subcommand from its registry spec.
func newServiceCmd(name string, flags *rootFlags) *cobra.Command {
	_, spec, _ := config.GetService(name)
	cmd := &cobra.Command{
		Use:     name,
		Short:   fmt.Sprintf("Start %s (%s) via %s", name, spec.Type, strings.Join(spec.Runtimes(), ", ")),
		Long:    serviceHelp(name, spec),
		GroupID: "services",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd, name, flags)
		},
		SilenceUsage: true,
	}
	addStartFlags(cmd, flags, spec)
	return cmd
}

func addStartFlags(cmd *cobra.Command, flags *rootFlags, spec *types.ServiceSpec) {
	cmd.Flags().StringVar(&flags.runtime, "runtime", "", fmt.Sprintf("runtime to use: %s (default: first supported)", strings.Join(spec.Runtimes(), ", ")))
	cmd.Flags().StringVar(&flags.version, "version", "", "service version to install/run (default: latest)")
	cmd.Flags().IntVar(&flags.port, "port", 0, "host port override for the primary service port")
	cmd.Flags().StringVar(&flags.bind, "bind", "", "address the service listens on (default 127.0.0.1; use 0.0.0.0 for all interfaces)")
	cmd.Flags().StringVar(&flags.dataDir, "data-dir", "", "service data directory override")
	cmd.Flags().BoolVarP(&flags.detach, "detach", "d", false, "run the service in the background")
	cmd.Flags().DurationVar(&flags.waitTimeout, "wait-timeout", 2*time.Minute, "readiness wait timeout")
	cmd.Flags().StringVarP(&flags.output, "output", "o", "yaml", "connection output format: yaml, json or env")
	if spec.Helm != nil {
		cmd.Flags().StringVarP(&flags.namespace, "namespace", "n", "default", "kubernetes namespace (helm runtime)")
	} else {
		flags.namespace = "default"
	}
}

// serviceHelp renders the long help for a service from its spec.
func serviceHelp(name string, spec *types.ServiceSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Start %s and print its %s connection.\n", name, spec.Type)
	fmt.Fprintf(&b, "Pin a version with %s@<version> (same constraint semantics as deps install).\n\n", name)
	fmt.Fprintf(&b, "Runtimes: %s\n", strings.Join(spec.Runtimes(), ", "))
	var ports []string
	for _, p := range spec.Ports {
		ports = append(ports, fmt.Sprintf("%s=%d", p.Name, p.Port))
	}
	if len(ports) > 0 {
		fmt.Fprintf(&b, "Ports:    %s\n", strings.Join(ports, ", "))
	}
	if creds := spec.Credentials; creds != nil {
		fmt.Fprintf(&b, "Username: %s\n", creds.Username)
		if creds.Database != "" {
			fmt.Fprintf(&b, "Database: %s\n", creds.Database)
		}
	}
	if spec.Helm != nil {
		chart := spec.Helm.Chart
		if spec.Helm.Repo != "" {
			chart = spec.Helm.Repo + " " + chart
		}
		fmt.Fprintf(&b, "Chart:    %s\n", chart)
	}
	if spec.Docker != nil {
		fmt.Fprintf(&b, "Image:    %s\n", spec.Docker.Image)
	}
	return strings.TrimRight(b.String(), "\n")
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
	if f.bind != "" {
		opts = append(opts, start.WithBindAddress(f.bind))
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

	// SIGHUP restarts the supervised service in place (deps-start restart
	// signals detached supervisors this way)
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for range hup {
			if err := instance.Restart(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "restart failed: %v\n", err)
			}
		}
	}()

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
