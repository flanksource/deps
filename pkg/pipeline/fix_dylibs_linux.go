//go:build linux

package pipeline

import (
	"fmt"
	"os/exec"
	"strings"
)

func detectBrokenDylibs(binaryPath, _ string) ([]DylibRef, error) {
	out, err := exec.Command("ldd", binaryPath).Output()
	if err != nil {
		return nil, fmt.Errorf("ldd failed: %w", err)
	}
	return parseLddOutput(string(out)), nil
}

func parseLddOutput(output string) []DylibRef {
	var refs []DylibRef
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "=> not found") {
			parts := strings.SplitN(line, " =>", 2)
			refs = append(refs, DylibRef{Path: strings.TrimSpace(parts[0]), Found: false})
		}
	}
	return refs
}

func machoArchToGoArch(arch string) string {
	return arch
}

// detectArch is a no-op on linux — ELF shared libs don't have the same arch mismatch issue
// because the linker resolves by matching ELF class/machine type automatically.
func detectArch(path string) string {
	return ""
}
