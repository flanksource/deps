package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api/icons"

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/start"
)

type rootFlags struct {
	stateDir string
}

type startFlags struct {
	root          *rootFlags
	runtime       string
	version       string
	port          int
	bind          string
	namespace     string
	namespaceFlag *pflag.Flag
	dataDir       string
	volumeMode    string
	detach        bool
	waitTimeout   time.Duration
	output        string
	parameters    map[string]*pflag.Flag
	parameterErr  error
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
	for _, sub := range []*cobra.Command{newStopCmd(flags), newRestartCmd(flags), newStatusCmd(flags), newInfoCmd(flags), newListCmd(flags), newLogsCmd(flags)} {
		sub.GroupID = "management"
		cmd.AddCommand(sub)
	}
	applyHelpStyling(cmd)
	return cmd
}

// newServiceCmd builds the per-service subcommand from its registry spec.
func newServiceCmd(name string, flags *rootFlags) *cobra.Command {
	_, spec, _ := config.GetService(name)
	serviceFlags := &startFlags{root: flags, parameters: map[string]*pflag.Flag{}}
	cmd := &cobra.Command{
		Use:     name,
		Short:   fmt.Sprintf("Start %s (%s) via %s", name, spec.Type, strings.Join(spec.Runtimes(), ", ")),
		Long:    serviceHelp(name, spec),
		GroupID: "services",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd, name, serviceFlags)
		},
		SilenceUsage: true,
	}
	addStartFlags(cmd, serviceFlags, spec)
	return cmd
}

func addStartFlags(cmd *cobra.Command, flags *startFlags, spec *types.ServiceSpec) {
	cmd.Flags().StringVar(&flags.runtime, "runtime", "", fmt.Sprintf("runtime to use: %s (default: first supported)", strings.Join(spec.Runtimes(), ", ")))
	cmd.Flags().StringVar(&flags.version, "version", "", "service version to install/run (default: latest)")
	cmd.Flags().IntVar(&flags.port, "port", 0, "primary service port override")
	cmd.Flags().StringVar(&flags.bind, "bind", "", "address the service listens on (default 127.0.0.1; use 0.0.0.0 for all interfaces)")
	cmd.Flags().StringVar(&flags.dataDir, "data-dir", "", "service data directory override")
	cmd.Flags().StringVar(&flags.volumeMode, "volume-mode", "", "primary data volume mode: persistent, host or ephemeral")
	cmd.Flags().BoolVarP(&flags.detach, "detach", "d", false, "run the service in the background")
	cmd.Flags().DurationVar(&flags.waitTimeout, "wait-timeout", 2*time.Minute, "readiness wait timeout")
	cmd.Flags().StringVarP(&flags.output, "output", "o", "json", "service output format: json, yaml or env")
	if spec.Helm != nil {
		cmd.Flags().StringVarP(&flags.namespace, "namespace", "n", "default", "kubernetes namespace (helm runtime)")
		flags.namespaceFlag = cmd.Flags().Lookup("namespace")
	} else {
		flags.namespace = "default"
	}
	if err := start.ValidateServiceParameters(*spec); err != nil {
		flags.parameterErr = err
		return
	}
	flags.parameterErr = addServiceParameterFlags(cmd, flags, spec.Parameters)
}

func addServiceParameterFlags(cmd *cobra.Command, flags *startFlags, parameters map[string]types.ServiceParameter) error {
	if flags.parameters == nil {
		flags.parameters = map[string]*pflag.Flag{}
	}
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if cmd.Flags().Lookup(name) != nil {
			return fmt.Errorf("parameter --%s collides with an existing flag", name)
		}
		parameter := parameters[name]
		help := parameter.Description
		if len(parameter.Runtimes) > 0 {
			help += " (" + strings.Join(parameter.Runtimes, ", ") + " runtime)"
		}
		switch parameter.Type {
		case types.ServiceParameterBool:
			value, err := strconv.ParseBool(parameter.Default)
			if err != nil {
				return fmt.Errorf("invalid default for --%s: %w", name, err)
			}
			cmd.Flags().Bool(name, value, help)
		case types.ServiceParameterInt:
			value, err := strconv.Atoi(parameter.Default)
			if err != nil {
				return fmt.Errorf("invalid default for --%s: %w", name, err)
			}
			cmd.Flags().Int(name, value, help)
		case types.ServiceParameterDuration:
			value, err := time.ParseDuration(parameter.Default)
			if err != nil {
				return fmt.Errorf("invalid default for --%s: %w", name, err)
			}
			cmd.Flags().Duration(name, value, help)
		case types.ServiceParameterString, types.ServiceParameterQuantity:
			cmd.Flags().String(name, parameter.Default, help)
		default:
			return fmt.Errorf("parameter --%s has unsupported type %q", name, parameter.Type)
		}
		flags.parameters[name] = cmd.Flags().Lookup(name)
	}
	return nil
}

func (f *rootFlags) options() []start.Option {
	if f.stateDir == "" {
		return nil
	}
	return []start.Option{start.WithStateDir(f.stateDir)}
}

func (f *startFlags) options() []start.Option {
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
	if f.volumeMode != "" {
		opts = append(opts, start.WithVolumeMode(start.VolumeMode(f.volumeMode)))
	}
	if f.root.stateDir != "" {
		opts = append(opts, start.WithStateDir(f.root.stateDir))
	}
	if f.namespaceFlag != nil && f.namespaceFlag.Changed {
		opts = append(opts, start.WithNamespace(f.namespace))
	}
	if parameters := f.parameterValues(); len(parameters) > 0 {
		opts = append(opts, start.WithParameters(parameters))
	}
	opts = append(opts,
		start.WithDetach(f.detach),
		start.WithWaitTimeout(f.waitTimeout),
	)
	return opts
}

func (f *startFlags) parameterValues() map[string]string {
	values := make(map[string]string, len(f.parameters))
	for name, flag := range f.parameters {
		if flag.Changed {
			values[name] = flag.Value.String()
		}
	}
	return values
}

func runStart(cmd *cobra.Command, name string, flags *startFlags) error {
	if flags.parameterErr != nil {
		return flags.parameterErr
	}
	if flags.detach && os.Getenv(supervisorEnv) == "" {
		if err := runDetached(name, flags); err != nil {
			return err
		}
		instance, err := start.Get(cmd.Context(), name, flags.options()...)
		if err != nil {
			return err
		}
		info, err := instance.Info(cmd.Context())
		if err != nil {
			return err
		}
		return writeServiceOutput(os.Stdout, info, flags.output)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	instance, err := start.Start(ctx, name, flags.options()...)
	if err != nil {
		return err
	}
	if err := writeConfigChange(os.Stderr, instance.Change, isTTY(os.Stderr)); err != nil {
		return err
	}
	printAction(icons.Success, "text-green-600", "%s %s (%s)", instance.Action, name, instance.Runtime)

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

	info, err := instance.Info(cmd.Context())
	if err != nil {
		return err
	}
	if err := writeServiceOutput(os.Stdout, info, flags.output); err != nil {
		return err
	}
	if err := instance.Wait(ctx); err != nil && ctx.Err() != nil {
		return nil // interrupted: the service was stopped cleanly
	} else if err != nil {
		return err
	}
	return nil
}

func writeServiceOutput(w io.Writer, info start.ServiceInfo, format string) error {
	switch format {
	case "yaml":
		return yaml.NewEncoder(w).Encode(info)
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	case "env":
		conn := info.Connection
		fmt.Fprintf(w, "URL=%s\n", conn.URL)
		if conn.Username != "" {
			fmt.Fprintf(w, "USERNAME=%s\n", conn.Username)
		}
		if conn.Password != "" {
			fmt.Fprintf(w, "PASSWORD=%s\n", conn.Password)
		}
		for k, v := range conn.Properties {
			fmt.Fprintf(w, "%s=%s\n", strings.ToUpper(k), v)
		}
		return nil
	default:
		return fmt.Errorf("unknown output format %q (json, yaml, env)", format)
	}
}

func writeConfigChange(w io.Writer, change *start.ConfigChange, ansi bool) error {
	if change == nil {
		return nil
	}
	diff := clicky.Diff(change.Before, change.After, "live", "desired")
	if ansi {
		_, err := fmt.Fprint(w, diff.ANSI())
		return err
	}
	_, err := fmt.Fprint(w, diff.String())
	return err
}
