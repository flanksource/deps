package runtime

import (
	"time"

	"github.com/flanksource/clicky/exec"
)

// RunOptions configures runtime execution behavior
type RunOptions struct {
	// Version constraint (e.g., ">=3.9", "18", "latest")
	// If empty, uses latest stable version
	Version string

	// Timeout for script execution
	Timeout time.Duration

	// WorkingDir sets the working directory for script execution
	WorkingDir string

	// Env provides custom environment variables
	Env map[string]string

	// Args provides additional command-line arguments to pass to the script
	Args []string

	// InstallDeps triggers dependency installation when true
	// When false, uses smart detection (only install if missing)
	InstallDeps *bool
}

// RunResult extends exec.Process with runtime-specific metadata
type RunResult struct {
	exec.Process

	// RuntimePath is the path to the runtime binary used
	RuntimePath string

	// RuntimeVersion is the version of the runtime used
	RuntimeVersion string
}

// runtimeInfo holds cached runtime information
type runtimeInfo struct {
	Path    string
	Version string
}
