//go:build darwin

package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func detectBrokenDylibs(binaryPath, binaryArch string) ([]DylibRef, error) {
	out, err := exec.Command("otool", "-L", binaryPath).Output()
	if err != nil {
		return nil, fmt.Errorf("otool -L failed: %w", err)
	}
	return parseOtoolOutput(string(out), binaryArch), nil
}

func parseOtoolOutput(output, binaryArch string) []DylibRef {
	var refs []DylibRef
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "(compatibility") {
			continue
		}
		// Format: "/path/to/lib.dylib (compatibility version X, current version Y)"
		parenIdx := strings.Index(line, " (compatibility")
		if parenIdx < 0 {
			continue
		}
		path := strings.TrimSpace(line[:parenIdx])
		if path == "" || strings.HasPrefix(path, "@") || strings.HasPrefix(path, "/usr/lib/lib") || strings.HasPrefix(path, "/System/") {
			continue
		}
		_, err := os.Stat(path)
		found := err == nil
		if found && binaryArch != "" {
			if libArch := detectArch(path); libArch != "" && libArch != binaryArch {
				found = false
			}
		}
		refs = append(refs, DylibRef{Path: path, Found: found})
	}
	return refs
}

func machoArchToGoArch(arch string) string {
	if arch == "x86_64" {
		return "amd64"
	}
	return arch
}

// detectArch returns the Mach-O architecture string (e.g. "arm64", "x86_64") for a binary.
func detectArch(path string) string {
	out, err := exec.Command("file", path).Output()
	if err != nil {
		return ""
	}
	s := string(out)
	for _, arch := range []string{"arm64", "x86_64"} {
		if strings.Contains(s, arch) {
			return arch
		}
	}
	return ""
}
