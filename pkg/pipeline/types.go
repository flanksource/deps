package pipeline

import (
	"strings"
)

// CELPipeline represents a series of CEL expressions to evaluate
type CELPipeline struct {
	RawExpression string   // Original expression string (for logging/debugging)
	Expressions   []string // Individual CEL expressions
	WorkDir       string   // Working directory (will be set by evaluator)
	Debug         bool     // Debug mode flag
}

// NewCELPipeline creates a CEL pipeline from a slice of expressions
func NewCELPipeline(expressions []string) *CELPipeline {
	if len(expressions) == 0 {
		return nil
	}

	// Clean up expressions
	var cleanExpressions []string
	for _, expr := range expressions {
		expr = strings.TrimSpace(expr)
		expr = strings.ReplaceAll(expr, "\n", " ")
		expr = strings.ReplaceAll(expr, "\t", " ")
		if expr != "" {
			cleanExpressions = append(cleanExpressions, expr)
		}
	}

	if len(cleanExpressions) == 0 {
		return nil
	}

	return &CELPipeline{
		RawExpression: strings.Join(cleanExpressions, "; "), // For logging/debugging
		Expressions:   cleanExpressions,
	}
}
