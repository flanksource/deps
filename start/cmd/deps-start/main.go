package main

import (
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/installer"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	os.Args = normalizeArgs(os.Args)
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var failure *startFailure
		if errors.As(err, &failure) {
			os.Exit(failure.code)
		}
		os.Exit(1)
	}
}

// startFailure carries the supervisor's own exit code so a failed background
// start exits with it rather than a generic 1.
type startFailure struct {
	code int
	err  error
}

func (e *startFailure) Error() string { return e.err.Error() }
func (e *startFailure) Unwrap() error { return e.err }

// supervisorExitCode maps the supervisor's wait error to the code deps-start
// should exit with. A supervisor that exits cleanly without ever publishing a
// ready state still failed to start the service, so it reports 1.
func supervisorExitCode(err error) int {
	var exit *osexec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return exit.ExitCode()
	}
	return 1
}

// normalizeArgs rewrites the explicit start verb and the name@version syntax
// onto the generated per-service command:
//
//	deps-start start postgres@17 --port 6000
//	  -> deps-start postgres --version 17 --port 6000
//
// Each service is its own cobra command so it can carry the flags derived
// from its registry spec; `start` is a second spelling of the same thing,
// there so start and stop read symmetrically.
func normalizeArgs(args []string) []string {
	return expandVersionArg(stripStartVerb(args))
}

// stripStartVerb drops a leading "start" when a known service name follows
// it. Anything else — no service, `--help`, an unknown name — is left for the
// start command itself to report.
func stripStartVerb(args []string) []string {
	if len(args) < 3 || args[1] != "start" {
		return args
	}
	if _, _, ok := config.GetService(strings.SplitN(args[2], "@", 2)[0]); !ok {
		return args
	}
	return append([]string{args[0]}, args[2:]...)
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
