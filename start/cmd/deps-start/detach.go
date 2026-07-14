//go:build unix

package main

import (
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/flanksource/clicky/api/icons"

	"github.com/flanksource/deps/start"
	"github.com/flanksource/deps/start/state"
)

const supervisorEnv = "DEPS_START_SUPERVISOR"

// runDetached re-execs deps-start in the background as a session leader,
// waits until the child publishes a ready state, prints the connection and
// returns. The child runs the normal foreground path and stays alive as the
// service supervisor.
func runDetached(name string, flags *startFlags) error {
	options, err := start.ResolveOptions(flags.options())
	if err != nil {
		return err
	}
	dir, err := state.Dir(options.StateDir, name)
	if err != nil {
		return err
	}
	logPath := filepath.Join(dir, "logs", "supervisor.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = logf.Close() }()

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	child := osexec.Command(exe, stripDetachFlag(os.Args[1:])...)
	child.Env = append(os.Environ(), supervisorEnv+"=1")
	child.Stdout, child.Stderr = logf, logf
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	spawned := time.Now()
	if err := child.Start(); err != nil {
		return err
	}
	pid := child.Process.Pid
	if err := child.Process.Release(); err != nil {
		return err
	}

	printAction(icons.Play, "muted", "starting %s in the background (supervisor pid %d, log %s)", name, pid, logPath)
	return awaitDetachedReady(name, options.StateDir, logPath, pid, spawned, options.WaitTimeout)
}

// awaitDetachedReady polls the state file until the child publishes a ready
// state newer than the spawn time, or the child dies.
func awaitDetachedReady(name, stateDir, logPath string, pid int, spawned time.Time, waitTimeout time.Duration) error {
	// installs can dominate startup, so allow extra headroom over the
	// readiness timeout the child itself enforces
	deadline := time.Now().Add(waitTimeout + 90*time.Second)
	stateFile := filepath.Join(stateDir, name, "state.yaml")
	for time.Now().Before(deadline) {
		if info, err := os.Stat(stateFile); err == nil && info.ModTime().After(spawned) {
			if st, err := state.Load(stateDir, name); err == nil && st.Ready {
				return nil
			}
		}
		if syscall.Kill(pid, 0) != nil {
			return fmt.Errorf("background start of %s failed, see %s:\n%s", name, logPath, tailFile(logPath, 20))
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s to become ready, see %s", name, logPath)
}

func stripDetachFlag(args []string) []string {
	var out []string
	for _, arg := range args {
		if arg == "-d" || arg == "--detach" || arg == "--detach=true" {
			continue
		}
		out = append(out, arg)
	}
	return out
}
