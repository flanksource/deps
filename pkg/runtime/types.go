package runtime

import (
	"time"

	"github.com/flanksource/clicky/exec"
)

// RunOptions configures runtime execution behavior
type RunOptions struct {
	// Version constraint (e.g., ">=3.9", "18", "latest")
	// If empty, uses latest stable version
	Version string `json:"version,omitempty" flag:"version"`

	// Timeout for script execution
	Timeout time.Duration `json:"timeout,omitempty" flag:"timeout"`

	// WorkingDir sets the working directory for script execution
	WorkingDir string `json:"working_dir,omitempty" flag:"working-dir"`

	// Env provides custom environment variables
	Env map[string]string `json:"env,omitempty" flag:"env"`

	// Args provides additional command-line arguments to pass to the script
	Args []string `json:"args,omitempty" args:"true"`

	// InstallDeps triggers dependency installation when true
	// When false, uses smart detection (only install if missing)
	InstallDeps *bool `json:"install_deps,omitempty" flag:"install" default:"false"`
}

// RunResult extends exec.Process with runtime-specific metadata
type RunResult struct {
	*exec.Process

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
