package version

import (
	"strings"

	"github.com/flanksource/gomplate/v3"
)

// EvaluateVersionFallback evaluates a version_fallback CEL expression to transform the version
// before URL templating. Returns the original version and tag if expr is empty.
func EvaluateVersionFallback(expr, version, tag, os, arch string) (string, string, error) {
	if strings.TrimSpace(expr) == "" {
		return version, tag, nil
	}

	data := map[string]interface{}{
		"version": version,
		"tag":     tag,
		"os":      os,
		"arch":    arch,
	}

	result, err := gomplate.RunTemplate(data, gomplate.Template{Expression: strings.TrimSpace(expr)})
	if err != nil {
		return "", "", err
	}

	if result == "" || result == version {
		return version, tag, nil
	}

	newTag := result
	if strings.HasPrefix(tag, "v") && !strings.HasPrefix(result, "v") {
		newTag = "v" + result
	}

	return result, newTag, nil
}
