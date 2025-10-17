package version

import (
	"path/filepath"
	"strings"

	clickyExec "github.com/flanksource/clicky/exec"
	"github.com/flanksource/clicky/task"
)

// buildVersionCheckProcess creates an exec.Process configured for version checking
// based on the mode (shell command, directory, or binary)
func buildVersionCheckProcess(
	cmdArgs []string,
	binaryPath string,
	binDir string,
	mode string,
	isShellCommand bool,
	versionCommand string,
	t *task.Task,
) (clickyExec.Process, error) {
	p := clickyExec.Process{}
	p = p.WithTask(t)

	// Check if this is a shell command that needs special handling
	if isShellCommand {
		return buildShellProcess(versionCommand, binaryPath, mode, t), nil
	}

	if mode == "directory" {
		return buildDirectoryModeProcess(cmdArgs, binaryPath, binDir, t)
	}

	// Binary mode (default)
	p.Cmd = binaryPath
	p.Args = cmdArgs
	return p, nil
}

// buildShellProcess creates a process for shell commands (with pipes, redirects, etc.)
func buildShellProcess(versionCommand, binaryPath, mode string, t *task.Task) clickyExec.Process {
	p := clickyExec.Process{}
	p = p.WithTask(t)

	if t != nil {
		t.V(4).Infof("Detected shell operators, wrapping in bash -c")
	}

	if mode == "directory" {
		p.Cwd = binaryPath
	}

	p.Cmd = getShellBinary()
	p.Args = []string{"-c", versionCommand}

	return p
}

// buildDirectoryModeProcess creates a process for directory mode packages
func buildDirectoryModeProcess(cmdArgs []string, binaryPath, binDir string, t *task.Task) (clickyExec.Process, error) {
	p := clickyExec.Process{}
	p = p.WithTask(t)

	// Try to resolve the binary path if it's a relative path
	if len(cmdArgs) > 0 && strings.Contains(cmdArgs[0], "/") {
		if resolved, found := ResolveVersionCommandBinary(strings.Join(cmdArgs, " "), binaryPath, binDir, "directory"); found {
			p.Cmd = resolved
			p.Args = cmdArgs[1:]
			p.Cwd = binaryPath
			return p, nil
		}
	}

	// Original logic: check if there's exactly one item in the directory
	visibleEntries, err := getVisibleEntries(binaryPath)
	if err != nil {
		return p, err
	}

	if len(visibleEntries) == 1 {
		// Single item: cd into it and execute command
		singleItem := filepath.Join(binaryPath, visibleEntries[0].Name())
		p.Cmd = cmdArgs[0]
		p.Args = cmdArgs[1:]
		p.Cwd = singleItem
	} else {
		// Multiple items: stay in package directory and execute command
		p.Cmd = cmdArgs[0]
		p.Args = cmdArgs[1:]
		p.Cwd = binaryPath
	}

	return p, nil
}
