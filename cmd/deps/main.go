package main

import (
	"os"

	"github.com/flanksource/deps/cmd"
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
		os.Exit(1)
	}
}
