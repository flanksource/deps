package main

import (
	"errors"
	"os"

	"github.com/flanksource/deps/cmd"
	"github.com/flanksource/deps/pkg/installer"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
	dirty   = "false"
)

func main() {
	cmd.SetVersion(version, commit, date, dirty)
	if err := cmd.Execute(); err != nil {
		var vme *installer.VersionMismatchError
		if errors.As(err, &vme) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
