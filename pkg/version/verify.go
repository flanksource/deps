package version

import (
	"fmt"
	"strings"

	"github.com/flanksource/gomplate/v3"
)

// EvaluateVerifyExpr evaluates a verify_expr CEL expression to determine if the installed
// version is acceptable. Returns true if verification passes.
func EvaluateVerifyExpr(expr, installed, expected, output, os, arch string) (bool, error) {
	if strings.TrimSpace(expr) == "" {
		return false, fmt.Errorf("empty verify_expr")
	}

	data := map[string]interface{}{
		"installed": installed,
		"expected":  expected,
		"output":    output,
		"os":        os,
		"arch":      arch,
	}

	result, err := gomplate.RunTemplate(data, gomplate.Template{Expression: strings.TrimSpace(expr)})
	if err != nil {
		return false, fmt.Errorf("verify_expr evaluation failed: %w", err)
	}

	return result == "true", nil
}
