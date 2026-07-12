//go:build unix

package start

import (
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
// the grace period.
func killProcessGroup(pid int, grace time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("no pid recorded")
	}
	if !processAlive(pid) {
		return nil
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return syscall.Kill(-pid, syscall.SIGKILL)
}
