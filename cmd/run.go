package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/deps/pkg/runtime"
	"github.com/spf13/cobra"
)

type RunOptions struct {
	Version     string
	Timeout     string
	WorkingDir  string
	Env         map[string]string
	InstallDeps bool
	Script      string
	Runtime     string
}

var runOpts RunOptions

var runCmd = &cobra.Command{
	Use:   "run [SCRIPT] [ARGS...]",
	Short: "Execute scripts with automatic runtime detection",
	Long: `Execute scripts in multiple languages with automatic runtime detection and installation.

Supported languages:
  - Python (.py) - Auto-installs from requirements.txt
  - JavaScript (.js, .mjs, .cjs) - Auto-installs from package.json
  - TypeScript (.ts, .tsx) - Requires tsx or ts-node
  - Java (.java, .jar, .class) - Auto-compiles .java files
  - PowerShell (.ps1) - Uses pwsh or powershell

Examples:
  deps run script.py
  deps run --version ">=3.9" script.py
  deps run --timeout 30s server.js
  deps run --env "API_KEY=secret" script.py
  deps run script.py arg1 arg2
  deps run -e "print('hello')" --runtime python
  deps run -e "console.log('hello')" --runtime node
  deps run -e "console.log('hello')" --runtime node@20`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var scriptPath string
		var scriptArgs []string

		if runOpts.Script != "" {
			// Inline script mode
			if runOpts.Runtime == "" {
				return fmt.Errorf("--runtime is required when using -c/--script")
			}
			scriptPath = runOpts.Script
			scriptArgs = args
		} else {
			// File mode
			if len(args) < 1 {
				return fmt.Errorf("SCRIPT argument is required (or use -c/--script for inline scripts)")
			}
			scriptPath = args[0]
			scriptArgs = args[1:]
		}

		result, err := executeScript(scriptPath, scriptArgs, runOpts)
		if err != nil {
			// Print output even on error
			if result.Stdout != "" {
				fmt.Println(result.Stdout)
			}
			if result.Stderr != "" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), result.Stderr)
			}
			return err
		}

		// Print stdout
		if result.Stdout != "" {
			fmt.Println(result.Stdout)
		}

		// Print stderr to stderr
		if result.Stderr != "" {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), result.Stderr)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVarP(&runOpts.Script, "script", "e", "", "Inline script to execute (requires --runtime)")
	runCmd.Flags().StringVar(&runOpts.Runtime, "runtime", "", "Runtime to use (e.g., python, node, node@20, python@3.11)")
	runCmd.Flags().StringVar(&runOpts.Version, "version", "", "Runtime version constraint (e.g., '>=3.9', '18', 'latest')")
	runCmd.Flags().StringVar(&runOpts.Timeout, "timeout", "", "Script execution timeout (e.g., '30s', '5m')")
	runCmd.Flags().StringVar(&runOpts.WorkingDir, "working-dir", "", "Working directory for script execution")
	runCmd.Flags().StringToStringVar(&runOpts.Env, "env", nil, "Environment variables (key=value)")
	runCmd.Flags().BoolVar(&runOpts.InstallDeps, "install", false, "Install dependencies before running")
}

type RunResult struct {
	Script         string
	Runtime        string
	RuntimePath    string
	RuntimeVersion string
	ExitCode       int
	Stdout         string
	Stderr         string
	Error          string
}

func executeScript(scriptPath string, scriptArgs []string, opts RunOptions) (RunResult, error) {
	var runtimeType string
	var runtimeVersion string
	var err error
	var tempFile string

	// Parse runtime@version if specified
	if opts.Runtime != "" {
		runtimeType, runtimeVersion = parseRuntimeSpec(opts.Runtime)
	}

	// --version flag overrides version from runtime spec
	if opts.Version != "" {
		runtimeVersion = opts.Version
	}

	// Handle inline script vs file
	if opts.Script != "" {
		ext := runtimeExtension(runtimeType)
		f, err := os.CreateTemp("", "deps-script-*"+ext)
		if err != nil {
			return RunResult{}, fmt.Errorf("failed to create temp file: %w", err)
		}
		tempFile = f.Name()
		// nolint:errcheck
		defer os.Remove(tempFile)

		if _, err := f.WriteString(scriptPath); err != nil {
			_ = f.Close()
			return RunResult{}, fmt.Errorf("failed to write script: %w", err)
		}
		_ = f.Close()
		scriptPath = tempFile
	} else if runtimeType == "" {
		// Detect runtime from file extension
		runtimeType, err = detectRuntime(scriptPath)
		if err != nil {
			return RunResult{}, err
		}
	}

	// Build runtime.RunOptions
	runOpts := runtime.RunOptions{
		Version:    runtimeVersion,
		WorkingDir: opts.WorkingDir,
		Env:        opts.Env,
		Args:       scriptArgs,
	}

	// Handle timeout parsing
	if opts.Timeout != "" {
		timeout, err := time.ParseDuration(opts.Timeout)
		if err != nil {
			return RunResult{}, fmt.Errorf("invalid timeout: %w", err)
		}
		runOpts.Timeout = timeout
	}

	// Handle InstallDeps
	if opts.InstallDeps {
		installDeps := true
		runOpts.InstallDeps = &installDeps
	}

	// Execute script with appropriate runtime
	var runResult *runtime.RunResult
	switch runtimeType {
	case "python":
		runResult, err = runtime.RunPython(scriptPath, runOpts)
	case "node":
		runResult, err = runtime.RunNode(scriptPath, runOpts)
	case "java":
		runResult, err = runtime.RunJava(scriptPath, runOpts)
	case "powershell":
		runResult, err = runtime.RunPowershell(scriptPath, runOpts)
	default:
		return RunResult{}, fmt.Errorf("unsupported runtime: %s", runtimeType)
	}

	if err != nil {
		return RunResult{
			Script:   scriptPath,
			Runtime:  runtimeType,
			ExitCode: -1,
			Error:    err.Error(),
		}, err
	}

	// Build result
	result := RunResult{
		Script:         scriptPath,
		Runtime:        runtimeType,
		RuntimePath:    runResult.RuntimePath,
		RuntimeVersion: runResult.RuntimeVersion,
		ExitCode:       runResult.ExitCode(),
	}

	stdout := runResult.GetStdout()
	if stdout != "" {
		result.Stdout = stdout
	}

	stderr := runResult.GetStderr()
	if stderr != "" {
		result.Stderr = stderr
	}

	if runResult.Err != nil {
		result.Error = runResult.Err.Error()
	}

	return result, nil
}

// detectRuntime determines the runtime type based on file extension
func detectRuntime(scriptPath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(scriptPath))

	switch ext {
	case ".py":
		return "python", nil
	case ".js", ".mjs", ".cjs":
		return "node", nil
	case ".ts", ".tsx":
		return "node", nil
	case ".java", ".jar", ".class":
		return "java", nil
	case ".ps1":
		return "powershell", nil
	default:
		return "", fmt.Errorf("unsupported file extension: %s (supported: .py, .js, .ts, .java, .jar, .ps1)", ext)
	}
}

func runtimeExtension(runtimeType string) string {
	switch runtimeType {
	case "python":
		return ".py"
	case "node":
		return ".js"
	case "java":
		return ".java"
	case "powershell":
		return ".ps1"
	default:
		return ".txt"
	}
}

// parseRuntimeSpec parses "runtime@version" into runtime and version parts
func parseRuntimeSpec(spec string) (runtime, version string) {
	if idx := strings.Index(spec, "@"); idx != -1 {
		return spec[:idx], spec[idx+1:]
	}
	return spec, ""
}
