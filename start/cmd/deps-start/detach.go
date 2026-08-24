//go:build unix

package main

import (
	"context"
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

// daemonizeSupported reports whether a service can be left running in the
// background on this platform; starting one does so by default.
const daemonizeSupported = true

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
	// supervisorEnv puts the child on the foreground path; its arguments are
	// otherwise this invocation's, verbatim
	child := osexec.Command(exe, os.Args[1:]...)
	child.Env = append(os.Environ(), supervisorEnv+"=1")
	child.Stdout, child.Stderr = logf, logf
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// record where both logs end before spawning, so the tail below shows
	// this start rather than replaying earlier runs
	tails := []logTail{logTailFrom(filepath.Join(dir, "logs", "service.log")), logTailFrom(logPath)}

	spawned := time.Now()
	if err := child.Start(); err != nil {
		return err
	}
	pid := child.Process.Pid
	// reap the supervisor so an early failure is noticed immediately: an
	// unreaped child stays a zombie and still answers kill(pid, 0). It is a
	// session leader, so it outlives this process either way.
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	printAction(icons.Play, "muted", "starting %s in the background (supervisor pid %d, log %s)", name, pid, logPath)

	// the supervisor's stderr is the supervisor log, so follow both logs to
	// show the service starting and what it is waiting for
	stream := newPrefixWriter(os.Stderr, name, isTTY(os.Stderr))
	streamCtx, stopStream := context.WithCancel(context.Background())
	tailLogsUntil(streamCtx, stream, tails...)
	defer func() {
		stopStream()
		stream.Flush()
	}()

	// the child enforces the readiness timeout itself; only an --update run
	// can spend time downloading before that clock starts
	timeout := options.WaitTimeout
	if flags.update {
		timeout += 90 * time.Second
	}
	return awaitDetachedReady(name, options.StateDir, logPath, pid, exited, spawned, timeout)
}

// awaitDetachedReady polls the state file until the supervisor publishes a
// ready state newer than the spawn time, or the supervisor exits. On timeout
// the supervisor is left running: a slow service can still come up, and its
// progress stays visible through the logs.
func awaitDetachedReady(name, stateDir, logPath string, pid int, exited <-chan error, spawned time.Time, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	stateFile := filepath.Join(stateDir, name, "state.yaml")
	for time.Now().Before(deadline) {
		if detachedReady(stateDir, name, stateFile, spawned) {
			return nil
		}
		select {
		case err := <-exited:
			if detachedReady(stateDir, name, stateFile, spawned) {
				return nil
			}
			return &startFailure{
				code: supervisorExitCode(err),
				err:  fmt.Errorf("%s failed to start, see %s:\n%s", name, logPath, tailFile(logPath, 20)),
			}
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("timed out after %s waiting for %s to become ready; it is still starting (supervisor pid %d), follow it with `deps-start logs %s -f`",
		timeout, name, pid, name)
}

func detachedReady(stateDir, name, stateFile string, spawned time.Time) bool {
	info, err := os.Stat(stateFile)
	if err != nil || !info.ModTime().After(spawned) {
		return false
	}
	st, err := state.Load(stateDir, name)
	return err == nil && st.Ready
}
