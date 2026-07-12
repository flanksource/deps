//go:build windows

package start

import (
	"fmt"
	"time"
)

func processAlive(pid int) bool { return false }

func killProcessGroup(pid int, grace time.Duration) error {
	return fmt.Errorf("the binary runtime is not supported on windows")
}
