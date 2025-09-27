package main

import (
	"os"

	"github.com/flanksource/deps/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
