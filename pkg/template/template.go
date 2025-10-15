package template

import (
	"fmt"
	"strings"

	depsversion "github.com/flanksource/deps/pkg/version"
	"github.com/flanksource/gomplate/v3"
)

func NormalizeVersion(version string) string {
	return depsversion.Normalize(version)
}

// RenderTemplate renders a template string using flanksource/gomplate with CEL support
func RenderTemplate(templateStr string, data map[string]interface{}) (string, error) {
	// Use gomplate's RunTemplate which supports both go templates and CEL expressions
	result, err := gomplate.RunTemplate(data, gomplate.Template{
		Template: templateStr,
	})
	if err != nil {
		return "", fmt.Errorf("template execution failed: %w", err)
	}

	return result, nil
}

// RenderCELExpression renders a CEL expression using flanksource/gomplate
func RenderCELExpression(expression string, data map[string]interface{}) (string, error) {
	// Use gomplate's RunTemplate with CEL expression
	result, err := gomplate.RunTemplate(data, gomplate.Template{
		Expression: expression,
	})
	if err != nil {
		return "", fmt.Errorf("CEL expression execution failed: %w", err)
	}

	return result, nil
}

// RenderTemplateWithCEL renders a template that may contain both Go template syntax and CEL expressions
func RenderTemplateWithCEL(templateStr, celExpression string, data map[string]interface{}) (string, error) {
	// Use gomplate's RunTemplate with both template and CEL expression
	result, err := gomplate.RunTemplate(data, gomplate.Template{
		Template:   templateStr,
		Expression: celExpression,
	})
	if err != nil {
		return "", fmt.Errorf("template with CEL execution failed: %w", err)
	}

	return result, nil
}

// Backwards compatibility functions that match the existing interface
func TemplateString(pattern string, data map[string]string) (string, error) {
	// Convert map[string]string to map[string]interface{}
	interfaceData := make(map[string]interface{})
	for k, v := range data {
		interfaceData[k] = v
	}
	return RenderTemplate(pattern, interfaceData)
}

// TemplateURL templates a URL with version and platform variables (backwards compatibility)
func TemplateURL(urlTemplate, version, os, arch string) (string, error) {
	data := map[string]interface{}{
		"version": depsversion.Normalize(version), // normalized without "v" prefix
		"tag":     version,                        // original tag format
		"os":      os,
		"arch":    arch,
	}
	return RenderTemplate(urlTemplate, data)
}

// TemplateURLWithAsset templates a URL with version, platform, and asset variables
func TemplateURLWithAsset(urlTemplate, version, os, arch, asset string) (string, error) {
	data := map[string]interface{}{
		"version": depsversion.Normalize(version), // normalized without "v" prefix
		"tag":     version,                        // original tag format
		"os":      os,
		"arch":    arch,
		"asset":   asset, // resolved asset name
	}
	return RenderTemplate(urlTemplate, data)
}

// TemplateStringWithCEL provides CEL-based templating for backwards compatibility
func TemplateStringWithCEL(celExpression string, data map[string]string) (string, error) {
	// Convert map[string]string to map[string]interface{}
	interfaceData := make(map[string]interface{})
	for k, v := range data {
		interfaceData[k] = v
	}
	return RenderCELExpression(celExpression, interfaceData)
}

// isCELExpression checks if a string looks like a CEL expression
func isCELExpression(expr string) bool {
	// Check for multiline expressions or common CEL operators
	return strings.Contains(expr, "\n") ||
		strings.Contains(expr, " ? ") ||
		strings.Contains(expr, " in ") ||
		strings.Contains(expr, "==") ||
		strings.Contains(expr, "!=")
}

// EvaluateCELOrTemplate evaluates a string as CEL if it looks like CEL, otherwise as a template
func EvaluateCELOrTemplate(expr string, data map[string]interface{}) (string, error) {
	if isCELExpression(expr) {
		return RenderCELExpression(expr, data)
	}
	return RenderTemplate(expr, data)
}
