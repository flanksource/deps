//go:build unix

package start

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// serviceProcessAlive reports whether pid is still the process of a service deps-start recorded
// at startedAt: a process has the PID, and it started no later than that. A state kept over a
// reboot, or restored from a CI cache onto another machine, names a PID that a newer, unrelated
// process can have. A process whose start time can't be read is taken at its PID.
func serviceProcessAlive(pid int, startedAt time.Time) bool {
	if !processAlive(pid) {
		return false
	}
	if startedAt.IsZero() {
		return true
	}
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}
	created, err := proc.CreateTime()
	if err != nil {
		return true
	}
	// A process's start time is read to about a second; a newer process with the PID started
	// well after the service was recorded.
	return time.UnixMilli(created).Before(startedAt.Add(2 * time.Second))
}

// signalSupervisor asks a supervising process to restart its service.
func signalSupervisor(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("failed to signal supervisor %d: %w", pid, err)
	}
	return nil
}

// killProcessGroup terminates a process group: SIGTERM, then SIGKILL after
// the grace period. Liveness is probed on the group (not the leader pid,
// which can linger as a zombie), and ESRCH means already-stopped, which is
// the desired end state.
func killProcessGroup(pid int, grace time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("no pid recorded")
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); errors.Is(err, syscall.ESRCH) {
		return nil
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
