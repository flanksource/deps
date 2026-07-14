package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"

	"github.com/flanksource/deps/start"
	"github.com/flanksource/deps/start/state"
)

// statusRow is the clicky TableProvider view of a service for the status and
// list commands. Empty cells collapse their column (WithoutEmptyColumns), so
// runtimes without live metrics simply omit CPU/MEM rather than showing blanks.
type statusRow struct {
	name     string
	runtime  start.RuntimeKind
	status   state.Status
	version  string
	image    string
	params   map[string]string
	volume   *state.Volume
	ports    []int
	cpu      float64
	hasCPU   bool
	rss      uint64
	restarts int
	started  time.Time
	url      string
}

func (r statusRow) Columns() []api.ColumnDef {
	return []api.ColumnDef{
		clicky.Column("name").Label("NAME").Style("font-bold").Build(),
		clicky.Column("runtime").Label("RUNTIME").Build(),
		clicky.Column("status").Label("STATUS").Build(),
		clicky.Column("version").Label("VERSION").Build(),
		clicky.Column("image").Label("IMAGE").Build(),
		clicky.Column("params").Label("PARAMS").Build(),
		clicky.Column("volume").Label("VOLUME").Build(),
		clicky.Column("ports").Label("PORTS").Build(),
		clicky.Column("cpu").Label("CPU").Build(),
		clicky.Column("mem").Label("MEM").Build(),
		clicky.Column("uptime").Label("UPTIME").Build(),
		clicky.Column("url").Label("URL").Build(),
	}
}

func (r statusRow) Row() map[string]any {
	return map[string]any{
		"name":    r.name,
		"runtime": string(r.runtime),
		"status":  r.statusText(),
		"version": r.version,
		"image":   r.image,
		"params":  joinParameters(r.params),
		"volume":  volumeSummary(r.volume),
		"ports":   joinPorts(r.ports),
		"cpu":     r.cpuText(),
		"mem":     r.memText(),
		"uptime":  r.uptimeText(),
		"url":     clicky.Text(r.url, "text-blue-500"),
	}
}

// statusText pairs a lifecycle icon with the coloured status word and, when a
// supervisor has restarted the service, a trailing restart count.
func (r statusRow) statusText() api.Text {
	icon, style := icons.Circle, "muted"
	switch r.status {
	case state.StatusRunning:
		icon, style = icons.Play, "text-green-600"
	case state.StatusStopped:
		icon, style = icons.Stop, "muted"
	}
	text := api.Text{}.Add(icon).Space().Append(string(r.status), style)
	if r.restarts > 0 {
		text = text.Space().Appendf("(%d restarts)", r.restarts).Styles("warning")
	}
	return text
}

func (r statusRow) cpuText() api.Text {
	if !r.hasCPU {
		return api.Text{}
	}
	return clicky.Text(fmt.Sprintf("%.1f%%", r.cpu), cpuStyle(r.cpu))
}

func (r statusRow) memText() api.Text {
	if r.rss == 0 {
		return api.Text{}
	}
	return api.HumanizeBytes(int64(r.rss)).Styles("muted")
}

func (r statusRow) uptimeText() api.Text {
	if r.started.IsZero() || r.status != state.StatusRunning {
		return api.Text{}
	}
	return clicky.Human(time.Since(r.started).Round(time.Second), "muted")
}

// cpuStyle escalates the colour as CPU usage rises so a hot service stands out.
func cpuStyle(pct float64) string {
	switch {
	case pct >= 80:
		return "text-red-600"
	case pct >= 50:
		return "warning"
	default:
		return "muted"
	}
}

func printStatusTable(cmd *cobra.Command, flags *rootFlags, instances []*start.Instance) error {
	rows := make([]statusRow, 0, len(instances))
	for _, i := range instances {
		status, err := start.Status(cmd.Context(), i.Name, flags.options()...)
		if err != nil {
			status = state.StatusUnknown
		}
		row := statusRow{
			name:    i.Name,
			runtime: i.Runtime,
			status:  status,
			version: i.State.Version,
			ports:   portValues(i.State.Ports),
			started: i.State.StartedAt,
			url:     i.Connection.String(),
		}
		if config := i.State.EffectiveConfig; config != nil {
			row.image, row.params, row.volume = config.Image, config.Parameters, config.Volume
			if row.image == "" {
				row.image = config.Chart
			}
		}
		if status == state.StatusRunning {
			if res := i.Metrics(cmd.Context()); res != nil {
				if len(res.Ports) > 0 {
					row.ports = res.Ports
				}
				row.cpu, row.hasCPU = res.CPUPercent, true
				row.rss = res.RSSBytes
				row.restarts = res.Restarts
			}
		}
		rows = append(rows, row)
	}
	fmt.Fprintln(os.Stdout, renderTextable(api.NewTableFrom(rows)))
	return nil
}

func joinParameters(parameters map[string]string) string {
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		names = append(names, name)
	}
	sort.Strings(names)
	values := make([]string, 0, len(names))
	for _, name := range names {
		values = append(values, name+"="+parameters[name])
	}
	return strings.Join(values, ",")
}

func volumeSummary(volume *state.Volume) string {
	if volume == nil {
		return ""
	}
	if volume.Source == "" {
		return volume.Mode + ":" + volume.Target
	}
	return volume.Mode + ":" + volume.Source + ":" + volume.Target
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

// renderTextable renders at the output boundary: coloured ANSI on a terminal,
// plain text when redirected to a file or pipe.
func renderTextable(t api.Textable) string {
	if isTTY(os.Stdout) {
		return t.ANSI()
	}
	return t.String()
}

// printAction writes an icon-prefixed status line to stderr, styling the
// message on a terminal and falling back to plain text when redirected.
func printAction(icon icons.Icon, style, format string, args ...any) {
	line := api.Text{}.Add(icon).Space().Append(fmt.Sprintf(format, args...), style)
	if isTTY(os.Stderr) {
		fmt.Fprintln(os.Stderr, line.ANSI())
	} else {
		fmt.Fprintln(os.Stderr, line.String())
	}
}

func isTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}
