package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky/task"
)

// DylibRef represents a dynamic library reference found in a binary.
type DylibRef struct {
	Path  string
	Found bool
}

var searchDirs = []string{
	"/opt/homebrew/opt/*/lib",
	"/opt/homebrew/lib",
	"/opt/homebrew/Cellar/*/*/lib",
	"/usr/local/opt/*/lib",
	"/usr/local/lib",
	"/usr/local/Cellar/*/*/lib",
	"/usr/local/Cellar/*/*/lib/postgresql",
	"/usr/lib",
	"/usr/lib/x86_64-linux-gnu",
	"/usr/lib/aarch64-linux-gnu",
	"/usr/local/lib/postgresql@*",
}

// findLibrary searches standard directories for a library matching basename.
// If targetArch is non-empty, only libraries matching that architecture are returned.
// When no compatible library is found, mismatchPath/mismatchArch report the first
// candidate that was skipped due to an architecture mismatch.
func findLibrary(basename, targetArch string) (path, mismatchPath, mismatchArch string) {
	for _, pattern := range searchDirs {
		var dirs []string
		if strings.ContainsAny(pattern, "*?[") {
			expanded, _ := filepath.Glob(pattern)
			dirs = expanded
		} else {
			dirs = []string{pattern}
		}
		for _, dir := range dirs {
			matches, _ := filepath.Glob(filepath.Join(dir, basename))
			for _, m := range matches {
				if _, err := os.Stat(m); err != nil {
					continue
				}
				if targetArch != "" {
					libArch := detectArch(m)
					if libArch != "" && libArch != targetArch {
						if mismatchPath == "" {
							mismatchPath = m
							mismatchArch = libArch
						}
						continue
					}
				}
				return m, "", ""
			}
		}
	}
	return "", mismatchPath, mismatchArch
}

// DetectBinaryArch returns the native architecture string for a binary
// (e.g. "arm64", "x86_64" on macOS; empty on unsupported platforms).
func DetectBinaryArch(path string) string {
	return detectArch(path)
}

// DiagnoseLibraryIssues detects broken dynamic library references on the installed
// binary and checks for architecture mismatches. Returns a human-readable diagnostic
// string with suggestions (e.g. --arch <alt>), or "" if no issues found.
func DiagnoseLibraryIssues(binaryPath string, t *task.Task) string {
	binaryArch := detectArch(binaryPath)
	refs, err := detectBrokenDylibs(binaryPath, binaryArch)
	if err != nil || len(refs) == 0 {
		return ""
	}

	var issues []string
	for _, ref := range refs {
		if ref.Found {
			continue
		}
		basename := filepath.Base(ref.Path)
		_, mismatchPath, mismatchArch := findLibrary(basename, binaryArch)
		if mismatchPath != "" {
			issues = append(issues, fmt.Sprintf(
				"  %s: found at %s but it is %s (binary is %s)\n  → try: deps install <pkg> --arch %s",
				basename, mismatchPath, mismatchArch, binaryArch, machoArchToGoArch(mismatchArch)))
		} else {
			issues = append(issues, fmt.Sprintf(
				"  %s: not found on system (expected at %s)", basename, ref.Path))
		}
	}

	if len(issues) == 0 {
		return ""
	}
	return fmt.Sprintf("Library issues detected:\n%s", strings.Join(issues, "\n"))
}
