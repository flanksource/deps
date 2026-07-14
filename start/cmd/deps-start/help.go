package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"

	"github.com/flanksource/deps/pkg/types"
)

// applyHelpStyling registers the template functions and colourised usage
// template so every command's help renders section headings and command names
// with clicky styling (plain when redirected).
func applyHelpStyling(cmd *cobra.Command) {
	cobra.AddTemplateFunc("heading", styleHeading)
	cobra.AddTemplateFunc("cmdname", styleCmdName)
	cmd.SetUsageTemplate(usageTemplate)
}

func styleHeading(s string) string {
	return renderTextable(clicky.Text(s, "font-bold text-cyan-600"))
}

func styleCmdName(s string) string {
	return renderTextable(clicky.Text(s, "text-green-600"))
}

// serviceHelp renders the long help for a service from its spec as an aligned,
// icon-prefixed key/value block.
func serviceHelp(name string, spec *types.ServiceSpec) string {
	t := api.Text{}.
		Add(icons.Play).Space().Append(name, "font-bold").
		Appendf(" — start %s and print its %s connection.", name, spec.Type).
		NewLine().
		Append(fmt.Sprintf("Pin a version with %s@<version> (same constraint semantics as deps install).", name), "muted").
		NewLine()

	t = helpField(t, "Runtimes", strings.Join(spec.Runtimes(), ", "), "text-cyan-600")

	var ports []string
	for _, p := range spec.Ports {
		ports = append(ports, fmt.Sprintf("%s=%d", p.Name, p.Port))
	}
	t = helpField(t, "Ports", strings.Join(ports, ", "), "")

	if creds := spec.Credentials; creds != nil {
		t = helpField(t, "Username", creds.Username, "")
		t = helpField(t, "Database", creds.Database, "")
	}
	if spec.Helm != nil {
		chart := spec.Helm.Chart
		if spec.Helm.Repo != "" {
			chart = spec.Helm.Repo + " " + chart
		}
		t = helpField(t, "Chart", chart, "muted")
	}
	if spec.Docker != nil {
		t = helpField(t, "Image", spec.Docker.Image, "muted")
	}
	var volumeModes []string
	if spec.Docker != nil && spec.Docker.DataPath != "" {
		volumeModes = append(volumeModes, "docker=host|persistent|ephemeral")
	}
	if spec.Helm != nil && spec.Helm.Volume != nil {
		modes := make([]string, 0, len(spec.Helm.Volume.Modes))
		for mode := range spec.Helm.Volume.Modes {
			modes = append(modes, mode)
		}
		sort.Strings(modes)
		volumeModes = append(volumeModes, "helm="+strings.Join(modes, "|"))
	}
	t = helpField(t, "Volumes", strings.Join(volumeModes, "; "), "muted")
	return renderTextable(t)
}

// helpField appends an aligned "label  value" line, skipping empty values so
// runtimes without credentials or charts simply omit those rows.
func helpField(t api.Text, label, value, valueStyle string) api.Text {
	if value == "" {
		return t
	}
	return t.NewLine().Append(fmt.Sprintf("%-10s", label+":"), "muted").Append(value, valueStyle)
}

// usageTemplate is cobra's default usage template with section headings and
// command names wrapped in clicky styling helpers.
const usageTemplate = `{{heading "Usage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{heading "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{heading "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{heading "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding | cmdname}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{heading $group.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding | cmdname}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{heading "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding | cmdname}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{heading "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{heading "Global Flags:"}}
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{heading "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding | cmdname}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
