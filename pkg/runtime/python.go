package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
)

var pythonVersionRegex = regexp.MustCompile(`Python\s+(\d+\.\d+(?:\.\d+)?)`)

// RunPython executes a Python script with automatic runtime detection and installation.
//
// Example:
//
//	result, err := runtime.RunPython("script.py", runtime.RunOptions{
//	    Version: ">=3.9",
//	    Timeout: 30 * time.Second,
//	})
func RunPython(script string, opts RunOptions) (*RunResult, error) {
	return RunPythonWithTask(script, opts, nil)
}

// RunPythonWithTask executes a Python script with a task for progress tracking
func RunPythonWithTask(script string, opts RunOptions, t *task.Task) (*RunResult, error) {
	detector := &runtimeDetector{
		language:       "python",
		binaryVariants: []string{"python3", "python"},
		versionCmd:     []string{"--version"},
		versionRegex:   pythonVersionRegex,
		task:           t,
	}

	// Find or install Python runtime
	runtimeInfo, err := detector.findOrInstallRuntime(opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to setup Python runtime: %w", err)
	}

	// Check for dependencies
	scriptDir := filepath.Dir(script)
	if err := installPythonDependencies(scriptDir, opts); err != nil {
		return nil, fmt.Errorf("failed to install Python dependencies: %w", err)
	}

	// Build execution command
	args := []string{script}
	args = append(args, opts.Args...)
	process := clicky.Exec(runtimeInfo.Path, args...)
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

// installPythonDependencies installs Python dependencies if needed
func installPythonDependencies(scriptDir string, opts RunOptions) error {
	// Check if we should install dependencies
	shouldInstall := false

	if opts.InstallDeps != nil {
		shouldInstall = *opts.InstallDeps
	} else {
		// Smart detection: check if requirements.txt or pyproject.toml exists
		requirementsTxt := filepath.Join(scriptDir, "requirements.txt")
		pyprojectToml := filepath.Join(scriptDir, "pyproject.toml")

		if fileExists(requirementsTxt) || fileExists(pyprojectToml) {
			shouldInstall = true
		}
	}

	if !shouldInstall {
		return nil
	}

	// Install dependencies
	requirementsTxt := filepath.Join(scriptDir, "requirements.txt")
	if fileExists(requirementsTxt) {
		// Use pip to install from requirements.txt
		process := clicky.Exec("pip", "install", "-r", requirementsTxt)

		if opts.WorkingDir != "" {
			process = process.WithCwd(opts.WorkingDir)
		}

		result := process.Run()
		if result.Err != nil {
			return result.Err
		}
	}

	// TODO: Handle pyproject.toml with pip install .

	return nil
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
