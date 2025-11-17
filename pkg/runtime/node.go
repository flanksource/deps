package runtime

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/task"
)

var nodeVersionRegex = regexp.MustCompile(`v?(\d+\.\d+(?:\.\d+)?)`)

// RunNode executes a Node.js script with automatic runtime detection and installation.
//
// Example:
//
//	result, err := runtime.RunNode("server.js", runtime.RunOptions{
//	    Version: ">=18.0",
//	    Timeout: 30 * time.Second,
//	})
func RunNode(script string, opts RunOptions) (*RunResult, error) {
	return RunNodeWithTask(script, opts, nil)
}

// RunNodeWithTask executes a Node.js script with a task for progress tracking
func RunNodeWithTask(script string, opts RunOptions, t *task.Task) (*RunResult, error) {
	// Handle npx: prefix for package execution
	if len(script) > 4 && script[:4] == "npx:" {
		return runNpx(script[4:], opts)
	}

	// Check if this is a TypeScript file
	isTypeScript := filepath.Ext(script) == ".ts" || filepath.Ext(script) == ".tsx"

	detector := &runtimeDetector{
		language:       "node",
		binaryVariants: []string{"node"},
		versionCmd:     []string{"--version"},
		versionRegex:   nodeVersionRegex,
		task:           t,
	}

	// Find or install Node runtime
	runtimeInfo, err := detector.findOrInstallRuntime(opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to setup Node runtime: %w", err)
	}

	// Check for dependencies
	scriptDir := filepath.Dir(script)
	if err := installNodeDependencies(scriptDir, opts); err != nil {
		return nil, fmt.Errorf("failed to install Node dependencies: %w", err)
	}

	// For TypeScript files, use tsx or ts-node
	var execPath string
	var args []string

	if isTypeScript {
		// Try to find tsx first (faster and more modern)
		tsxPath, _ := searchPath("tsx")
		if tsxPath != "" {
			execPath = tsxPath
			args = []string{script}
		} else {
			// Fall back to ts-node
			tsNodePath, _ := searchPath("ts-node")
			if tsNodePath != "" {
				execPath = tsNodePath
				args = []string{script}
			} else {
				return nil, fmt.Errorf("TypeScript execution requires tsx or ts-node. Install with: npm install -g tsx")
			}
		}
	} else {
		// Regular JavaScript execution
		execPath = runtimeInfo.Path
		args = []string{script}
	}

	args = append(args, opts.Args...)
	process := clicky.Exec(execPath, args...)
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

// runNpx executes a package via npx
func runNpx(packageAndArgs string, opts RunOptions) (*RunResult, error) {
	// Find npx
	npxPath, err := searchPath("npx")
	if err != nil {
		return nil, fmt.Errorf("npx not found in PATH")
	}

	process := clicky.Exec("npx", packageAndArgs)

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

	// Execute
	result := process.Run()

	// Build RunResult
	runResult := &RunResult{
		Process:        result,
		RuntimePath:    npxPath,
		RuntimeVersion: "npx",
	}

	return runResult, result.Err
}

// installNodeDependencies installs Node.js dependencies if needed
func installNodeDependencies(scriptDir string, opts RunOptions) error {
	// Check if we should install dependencies
	shouldInstall := false

	if opts.InstallDeps != nil {
		shouldInstall = *opts.InstallDeps
	} else {
		// Smart detection: check if package.json exists
		packageJSON := filepath.Join(scriptDir, "package.json")
		if fileExists(packageJSON) {
			shouldInstall = true
		}
	}

	if !shouldInstall {
		return nil
	}

	// Install dependencies with npm
	packageJSON := filepath.Join(scriptDir, "package.json")
	if fileExists(packageJSON) {
		process := clicky.Exec("npm", "install")

		if opts.WorkingDir != "" {
			process = process.WithCwd(opts.WorkingDir)
		} else {
			process = process.WithCwd(scriptDir)
		}

		result := process.Run()
		if result.Err != nil {
			return result.Err
		}
	}

	return nil
}
