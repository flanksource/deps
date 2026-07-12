//go:build windows

package main

import "fmt"

const supervisorEnv = "DEPS_START_SUPERVISOR"

func runDetached(name string, flags *rootFlags) error {
	return fmt.Errorf("--detach is not supported on windows")
}
