//go:build windows

package main

import "fmt"

const supervisorEnv = "DEPS_START_SUPERVISOR"

// daemonizeSupported reports whether a service can be left running in the
// background on this platform. Windows has no setsid equivalent here, so
// services run in the foreground and runStart says so.
const daemonizeSupported = false

func runDetached(name string, flags *startFlags) error {
	return fmt.Errorf("running %s in the background is not supported on windows", name)
}
