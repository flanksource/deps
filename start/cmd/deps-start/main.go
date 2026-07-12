package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/flanksource/deps/pkg/installer"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	os.Args = expandVersionArg(os.Args)
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// expandVersionArg rewrites "deps-start postgres@17 ..." into
// "deps-start postgres --version 17 ...", giving services the same
// name@version syntax and version-constraint semantics as deps install.
func expandVersionArg(args []string) []string {
	if len(args) < 2 || !strings.Contains(args[1], "@") {
		return args
	}
	tool := installer.ParseTools([]string{args[1]})[0]
	if tool.Version == "" {
		return args
	}
	return append([]string{args[0], tool.Name, "--version", tool.Version}, args[2:]...)
}
