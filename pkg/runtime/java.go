package runtime

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	clickyExec "github.com/flanksource/clicky/exec"
	"github.com/flanksource/clicky/task"
)

var javaVersionRegex = regexp.MustCompile(`version\s+"?(\d+(?:\.\d+)*)"?`)

// RunJava executes a Java file (.java or .jar) with automatic runtime detection and installation.
//
// For .jar files, executes with: java -jar file.jar
// For .java files, compiles first with javac, then runs with java
//
// Example:
//
//	result, err := runtime.RunJava("Main.jar", runtime.RunOptions{
//	    Version: ">=17",
//	    Timeout: 30 * time.Second,
//	    Env: map[string]string{"CLASSPATH": "./lib/*"},
//	})
func RunJava(script string, opts RunOptions) (*RunResult, error) {
	return RunJavaWithTask(script, opts, nil)
}

// RunJavaWithTask executes a Java file with a task for progress tracking
func RunJavaWithTask(script string, opts RunOptions, t *task.Task) (*RunResult, error) {
	detector := &runtimeDetector{
		language:       "java",
		binaryVariants: []string{"java"},
		versionCmd:     []string{"-version"},
		versionRegex:   javaVersionRegex,
		task:           t,
	}

	// Find or install Java runtime
	runtimeInfo, err := detector.findOrInstallRuntime(opts.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to setup Java runtime: %w", err)
	}

	// Determine execution type based on file extension
	ext := strings.ToLower(filepath.Ext(script))

	var process clickyExec.Process

	switch ext {
	case ".jar":
		// Execute JAR file
		args := []string{"-jar", script}
		args = append(args, opts.Args...)
		process = clickyExec.Process{
			Cmd:  runtimeInfo.Path,
			Args: args,
		}

	case ".java":
		// Compile and run Java source file
		// First, compile with javac
		javacPath, err := searchPath("javac")
		if err != nil {
			return nil, fmt.Errorf("javac not found in PATH (needed to compile .java files)")
		}

		// Get the directory and base name of the script
		scriptDir := filepath.Dir(script)
		scriptBase := filepath.Base(script)

		compileProcess := clickyExec.Process{
			Cmd:  javacPath,
			Args: []string{scriptBase},
		}

		if t != nil {
			compileProcess = compileProcess.WithTask(t)
		}

		// Set working directory to script directory for compilation
		workDir := opts.WorkingDir
		if workDir == "" && scriptDir != "" && scriptDir != "." {
			workDir = scriptDir
		}
		if workDir != "" {
			compileProcess = compileProcess.WithCwd(workDir)
		}

		compileResult := compileProcess.Run()
		if compileResult.Err != nil {
			return &RunResult{
				Process:        compileResult,
				RuntimePath:    javacPath,
				RuntimeVersion: runtimeInfo.Version,
			}, fmt.Errorf("compilation failed: %w\nStderr: %s", compileResult.Err, compileResult.Stderr.String())
		}

		// Get class name from filename (without .java extension)
		className := strings.TrimSuffix(scriptBase, ".java")

		// Run the compiled class with same working directory
		args := []string{className}
		args = append(args, opts.Args...)
		process = clickyExec.Process{
			Cmd:  runtimeInfo.Path,
			Args: args,
		}

		// Use same working directory for execution
		if workDir != "" {
			process = process.WithCwd(workDir)
		}

	case ".class":
		// Run compiled class file
		className := strings.TrimSuffix(filepath.Base(script), ".class")
		args := []string{className}
		args = append(args, opts.Args...)
		process = clickyExec.Process{
			Cmd:  runtimeInfo.Path,
			Args: args,
		}

	default:
		return nil, fmt.Errorf("unsupported Java file type: %s (expected .java, .jar, or .class)", ext)
	}

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

	// Execute
	result := process.Run()

	// Build RunResult
	runResult := &RunResult{
		Process:        result,
		RuntimePath:    runtimeInfo.Path,
		RuntimeVersion: runtimeInfo.Version,
	}

	return runResult, result.Err
}
