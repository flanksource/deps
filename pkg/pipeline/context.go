package pipeline

import (
	"fmt"

	"github.com/flanksource/clicky/task"
)

// PipelineContext provides context and control for CEL pipeline functions
type PipelineContext struct {
	// Task context for logging and progress
	Task *task.Task

	// Directory paths
	SandboxDir string // Current working directory where operations happen
	BinDir     string // Final destination directory for results
	TmpDir     string // Temporary directory for intermediate files

	// Configuration
	Debug bool // Whether to preserve files for debugging

	// Pipeline control
	Failed     bool   // Whether the pipeline has failed
	FailureMsg string // Failure message if pipeline failed
}

// FailPipeline marks the pipeline as failed with a specific message
func (ctx *PipelineContext) FailPipeline(message string) {
	ctx.Failed = true
	ctx.FailureMsg = message
	if ctx.Task != nil {
		// Use clean error message without redundant "Pipeline failed:" prefix
		cleanMsg := cleanErrorMessage(fmt.Errorf("%s", message))
		ctx.Task.Errorf("Pipeline failed: %s", cleanMsg)
	}
}

// LogInfo logs an info message if task context is available
func (ctx *PipelineContext) LogInfo(message string) {
	if ctx.Task != nil {
		ctx.Task.Infof("%s", message)
	}
}

// LogDebug logs a debug message if task context is available
func (ctx *PipelineContext) LogDebug(message string) {
	if ctx.Task != nil {
		ctx.Task.Debugf("%s", message)
	}
}

// LogError logs an error message if task context is available
func (ctx *PipelineContext) LogError(message string) {
	if ctx.Task != nil {
		ctx.Task.Errorf("%s", message)
	}
}

// CheckFailed returns true if the pipeline has failed
func (ctx *PipelineContext) CheckFailed() bool {
	return ctx.Failed
}

// GetFailureMessage returns the failure message if the pipeline has failed
func (ctx *PipelineContext) GetFailureMessage() string {
	if ctx.Failed {
		return ctx.FailureMsg
	}
	return ""
}

// NewPipelineContext creates a new pipeline context
func NewPipelineContext(t *task.Task, sandboxDir, binDir, tmpDir string, debug bool) *PipelineContext {
	return &PipelineContext{
		Task:       t,
		SandboxDir: sandboxDir,
		BinDir:     binDir,
		TmpDir:     tmpDir,
		Debug:      debug,
		Failed:     false,
		FailureMsg: "",
	}
}
