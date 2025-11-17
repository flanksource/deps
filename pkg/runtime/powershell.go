package runtime

import (
	"fmt"
	"regexp"
	"runtime"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
)

var powershellVersionRegex = regexp.MustCompile(`PowerShell\s+(\d+\.\d+(?:\.\d+)?)`)

// RunPowershell executes a PowerShell script with automatic runtime detection and installation.
//
// On Windows, uses powershell.exe or pwsh.exe
// On Unix, uses pwsh (PowerShell Core)
//
// Example:
//
//	result, err := runtime.RunPowershell("script.ps1", runtime.RunOptions{
//	    Version: ">=7.0",
//	    Timeout: 30 * time.Second,
//	})
func RunPowershell(script string, opts RunOptions) (*RunResult, error) {
	return RunPowershellWithTask(script, opts, nil)
}

// RunPowershellWithTask executes a PowerShell script with a task for progress tracking
func RunPowershellWithTask(script string, opts RunOptions, t *task.Task) (*RunResult, error) {
	// Determine binary variants based on OS
	var binaryVariants []string
	if runtime.GOOS == "windows" {
		// On Windows, try pwsh (PowerShell Core) first, then powershell (Windows PowerShell)
		binaryVariants = []string{"pwsh", "powershell"}
	} else {
		// On Unix, only pwsh is available
		binaryVariants = []string{"pwsh"}
	}

	detector := &runtimeDetector{
		language:       "powershell",
		binaryVariants: binaryVariants,
		versionCmd:     []string{"--version"},
		versionRegex:   powershellVersionRegex,
		task:           t,
	}

	// Find or install PowerShell runtime
	runtimeInfo, err := detector.findOrInstallRuntime(opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to setup PowerShell runtime: %w", err)
	}

	// Build execution command
	// Use -File to execute script file
	process := clicky.Exec(runtimeInfo.Path, "-File", script)

	// Apply options
	if opts.Timeout > 0 {
		process = process.WithTimeout(opts.Timeout)
	}

	if opts.WorkingDir != "" {
		process = process.WithCwd(opts.WorkingDir)
	}

	if opts.Env != nil {
		process = process.WithEnv(opts.Env)
	}

	if t != nil {
		process = process.WithTask(t)
	}

	// Execute script
	result := process.Run()

	// Build RunResult
	runResult := &RunResult{
		Process:        result,
		RuntimePath:    runtimeInfo.Path,
		RuntimeVersion: runtimeInfo.Version,
	}

	return runResult, result.Err
}
