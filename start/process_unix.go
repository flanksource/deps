//go:build unix

package start

import (
	"errors"
	"fmt"
	"syscall"
	"time"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
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
