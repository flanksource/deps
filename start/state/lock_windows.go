//go:build windows

package state

import "fmt"

// Lock is not supported on windows; deps-start service management is
// unix-only in v1.
func Lock(stateDir, name string) (func(), error) {
	return nil, fmt.Errorf("deps-start is not supported on windows")
}
