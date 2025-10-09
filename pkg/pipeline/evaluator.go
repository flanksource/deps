package pipeline

import (
	"errors"
	"fmt"
	"os"
	"time"

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

	// Check if workDir exists and is accessible
	if _, err := os.Stat(e.workDir); os.IsNotExist(err) {
		if e.task != nil {
			e.task.Debugf("WorkDir %s does not exist, nothing to process", e.workDir)
		}
		return nil // Empty workDir is not an error for pipeline operations
	}

	// Create pipeline context - work directly in workDir
	ctx := NewPipelineContext(e.task, e.workDir, e.binDir, e.tmpDir, e.debug)

	// Create CEL environment
	celEnv, err := NewCELPipelineEnvironment(ctx)
	if err != nil {
		return fmt.Errorf("failed to create CEL environment: %w", err)
	}

	// Execute each CEL expression in sequence
	for _, expr := range pipeline.Expressions {
		start := time.Now()
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
			e.task.V(4).Infof("Evaluated %s => %s (in %s)", expr, result, time.Since(start))
		}
	}

	// Check final pipeline state
	if ctx.CheckFailed() {
		return errors.New(ctx.GetFailureMessage())
	}

	// No move needed - installer handles all file placement
	return nil
}
