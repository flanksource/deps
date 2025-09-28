package pipeline

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// formatCELError formats CEL errors consistently with proper line breaks
func formatCELError(operation, expression string, err error) error {
	errMsg := err.Error()

	// Start CEL errors on a new line for better readability
	return fmt.Errorf("CEL %s failed for expression '%s':\n%s", operation, expression, errMsg)
}

// newCELError creates a properly formatted CEL error for CEL wrapper functions
func newCELError(operation string, err error) ref.Val {
	errMsg := err.Error()

	// Format with newline for better readability and remove redundant "error" text
	return types.NewErr("CEL %s failed:\n%s", operation, errMsg)
}

// handleFunctionError logs an error and fails the pipeline in one call
func handleFunctionError(ctx *PipelineContext, funcName string, err error) {
	// Log the error with function context
	ctx.LogError(fmt.Sprintf("CEL %s: %v", funcName, err))

	// Fail pipeline with clean error message (no redundant "failed" prefix)
	ctx.FailPipeline(err.Error())
}

// wrapWithContext adds context to an error without duplication
func wrapWithContext(err error, context string) error {
	errMsg := err.Error()

	// Avoid duplication if error already contains the context
	if strings.Contains(strings.ToLower(errMsg), strings.ToLower(context)) {
		return err
	}

	return fmt.Errorf("%s: %w", context, err)
}

// cleanErrorMessage removes redundant prefixes and formatting from error messages
func cleanErrorMessage(err error) string {
	errMsg := err.Error()

	// Remove common redundant prefixes
	redundantPrefixes := []string{
		"failed to ",
		"error: ",
		"pipeline failed: ",
	}

	for _, prefix := range redundantPrefixes {
		if strings.HasPrefix(strings.ToLower(errMsg), prefix) {
			errMsg = errMsg[len(prefix):]
			break
		}
	}

	return errMsg
}
