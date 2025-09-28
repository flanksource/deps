package pipeline

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/flanksource/clicky/task"
)

// CELPipelineEvaluator executes CEL-based pipeline expressions
type CELPipelineEvaluator struct {
	workDir string
	binDir  string
	tmpDir  string
	debug   bool
	task    *task.Task
}

// NewCELPipelineEvaluator creates a new CEL pipeline evaluator
func NewCELPipelineEvaluator(workDir, binDir, tmpDir string, t *task.Task, debug bool) *CELPipelineEvaluator {
	return &CELPipelineEvaluator{
		workDir: workDir,
		binDir:  binDir,
		tmpDir:  tmpDir,
		debug:   debug,
		task:    t,
	}
}

// Execute runs CEL pipeline expressions
func (e *CELPipelineEvaluator) Execute(pipeline *CELPipeline) error {
	if pipeline == nil || len(pipeline.Expressions) == 0 {
		return nil
	}

	// Create a sandbox directory for operations
	sandboxDir, err := os.MkdirTemp(e.tmpDir, "deps-pipeline-cel-")
	if err != nil {
		return fmt.Errorf("failed to create sandbox directory: %w", err)
	}

	// Clean up sandbox unless debug mode
	if !e.debug {
		defer os.RemoveAll(sandboxDir)
	} else if e.task != nil {
		e.task.Infof("Debug mode: keeping sandbox directory %s", sandboxDir)
	}

	// Copy extracted files from workDir to sandbox for pipeline operations
	if err := e.copyStagingToSandbox(sandboxDir); err != nil {
		return fmt.Errorf("failed to copy staging files to sandbox: %w", err)
	}

	// Create pipeline context
	ctx := NewPipelineContext(e.task, sandboxDir, e.binDir, e.tmpDir, e.debug)

	// Create CEL environment
	celEnv, err := NewCELPipelineEnvironment(ctx)
	if err != nil {
		return fmt.Errorf("failed to create CEL environment: %w", err)
	}

	// Execute each CEL expression in sequence
	for i, expr := range pipeline.Expressions {
		if e.task != nil {
			e.task.V(4).Infof("Pipeline: evaluating expression %d: %s", i+1, expr)
		}

		// Check if pipeline failed from previous expression
		if ctx.CheckFailed() {
			return errors.New(ctx.GetFailureMessage())
		}

		result, err := celEnv.Evaluate(expr)
		if err != nil {
			return err
		}

		// Log result if not nil
		if result != nil && e.task != nil {
			e.task.V(4).Infof("Expression result: %v", result)
		}
	}

	// Check final pipeline state
	if ctx.CheckFailed() {
		return errors.New(ctx.GetFailureMessage())
	}

	// Move final results to binDir
	return e.moveResults(sandboxDir)
}

// moveResults moves any files from sandbox to the final binDir
func (e *CELPipelineEvaluator) moveResults(sandboxDir string) error {
	entries, err := os.ReadDir(sandboxDir)
	if err != nil {
		return fmt.Errorf("failed to read sandbox directory: %w", err)
	}

	if len(entries) == 0 {
		if e.task != nil {
			e.task.Debugf("No files to move from sandbox")
		}
		return nil
	}

	if len(entries) == 1 && entries[0].IsDir() {
		// Single directory: move it as a whole for efficiency
		return e.moveSingleDirectory(sandboxDir, entries[0])
	} else {
		// Multiple items: move each entry individually
		return e.moveAllEntries(sandboxDir, entries)
	}
}

// moveSingleDirectory moves a single directory from sandbox to binDir as a whole unit
func (e *CELPipelineEvaluator) moveSingleDirectory(sandboxDir string, entry os.DirEntry) error {
	srcPath := filepath.Join(sandboxDir, entry.Name())
	dstPath := filepath.Join(e.binDir, entry.Name())

	if e.task != nil {
		e.task.V(3).Infof("Moving single directory %s to %s as whole unit", entry.Name(), e.binDir)
	}

	// Remove existing destination if it exists
	if _, err := os.Stat(dstPath); err == nil {
		if err := os.RemoveAll(dstPath); err != nil {
			return fmt.Errorf("failed to remove existing destination %s: %w", dstPath, err)
		}
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(e.binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Move the entire directory
	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to move directory %s to %s: %w", srcPath, dstPath, err)
	}

	if e.task != nil {
		e.task.V(3).Infof("Successfully moved directory %s to %s", entry.Name(), e.binDir)
	}

	return nil
}

// moveAllEntries moves all entries from sandbox to binDir individually
func (e *CELPipelineEvaluator) moveAllEntries(sandboxDir string, entries []os.DirEntry) error {
	if e.task != nil {
		e.task.V(3).Infof("Moving all entries (%d items) from sandbox to %s", len(entries), e.binDir)
	}

	// Ensure bin directory exists
	if err := os.MkdirAll(e.binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Move each entry to binDir
	for _, entry := range entries {
		srcPath := filepath.Join(sandboxDir, entry.Name())
		dstPath := filepath.Join(e.binDir, entry.Name())

		// Remove existing destination if it exists
		if _, err := os.Stat(dstPath); err == nil {
			if err := os.RemoveAll(dstPath); err != nil {
				return fmt.Errorf("failed to remove existing destination %s: %w", dstPath, err)
			}
		}

		// Move the file/directory
		if err := os.Rename(srcPath, dstPath); err != nil {
			return fmt.Errorf("failed to move %s to %s: %w", srcPath, dstPath, err)
		}

		if e.task != nil {
			if entry.IsDir() {
				e.task.V(3).Infof("Moved directory %s to %s", entry.Name(), e.binDir)
			} else {
				e.task.V(3).Infof("Moved file %s to %s", entry.Name(), e.binDir)
			}
		}
	}

	return nil
}

// copyStagingToSandbox copies all files from workDir to sandbox for pipeline operations
func (e *CELPipelineEvaluator) copyStagingToSandbox(sandboxDir string) error {
	// Check if workDir exists and is accessible
	workEntries, err := os.ReadDir(e.workDir)
	if err != nil {
		if e.task != nil {
			e.task.Debugf("WorkDir %s is empty or inaccessible: %v", e.workDir, err)
		}
		return nil // Empty workDir is not an error for pipeline operations
	}

	if len(workEntries) == 0 {
		if e.task != nil {
			e.task.Debugf("WorkDir %s is empty, nothing to copy to sandbox", e.workDir)
		}
		return nil
	}

	if e.task != nil {
		e.task.V(3).Infof("Pipeline: copying %d items from workDir %s to sandbox %s", len(workEntries), e.workDir, sandboxDir)
	}

	// Copy each entry from workDir to sandbox
	for i, entry := range workEntries {
		srcPath := filepath.Join(e.workDir, entry.Name())
		dstPath := filepath.Join(sandboxDir, entry.Name())

		if e.task != nil {
			e.task.V(4).Infof("Pipeline: copying item %d/%d: %s", i+1, len(workEntries), entry.Name())
		}

		// Copy file or directory
		if entry.IsDir() {
			if err := e.copyDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to copy directory %s: %w", entry.Name(), err)
			}
		} else {
			if err := e.copyFile(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to copy file %s: %w", entry.Name(), err)
			}
		}
	}

	if e.task != nil {
		e.task.V(3).Infof("Pipeline: successfully copied %d items", len(workEntries))
	}

	return nil
}

// copyFile copies a single file from src to dst
func (e *CELPipelineEvaluator) copyFile(srcPath, dstPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	// Copy file contents
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	// Copy file permissions
	if srcInfo, err := srcFile.Stat(); err == nil {
		os.Chmod(dstPath, srcInfo.Mode())
	}

	return nil
}

// copyDir recursively copies a directory from src to dst
func (e *CELPipelineEvaluator) copyDir(srcPath, dstPath string) error {
	// Create destination directory
	if err := os.MkdirAll(dstPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Read source directory entries
	entries, err := os.ReadDir(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	// Copy each entry recursively
	for _, entry := range entries {
		srcEntryPath := filepath.Join(srcPath, entry.Name())
		dstEntryPath := filepath.Join(dstPath, entry.Name())

		if entry.IsDir() {
			if err := e.copyDir(srcEntryPath, dstEntryPath); err != nil {
				return err
			}
		} else {
			if err := e.copyFile(srcEntryPath, dstEntryPath); err != nil {
				return err
			}
		}
	}

	return nil
}
