package pipeline

import (
	"fmt"
	"strings"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

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
