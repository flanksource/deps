package version

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/gomplate/v3"
)

// ApplyVersionExpr applies a CEL expression to filter and/or transform versions
// The expression is evaluated for each version with the following context:
//   - tag: Original tag name (string)
//   - version: Normalized version string (string)
//   - sha: Git SHA if available (string)
//   - published: Published timestamp (time.Time)
//   - prerelease: Whether this is a prerelease (bool)
//
// The CEL expression can:
//  1. Return a boolean to filter versions (true = include, false = exclude)
//  2. Return a modified version object to transform the version
//  3. Return the original version object unchanged
func ApplyVersionExpr(versions []types.Version, expr string) ([]types.Version, error) {
	if expr == "" {
		return versions, nil
	}

	var filteredVersions []types.Version

	for _, version := range versions {
		// Prepare context data for CEL evaluation
		data := map[string]interface{}{
			"tag":        version.Tag,
			"version":    version.Version,
			"sha":        version.SHA,
			"published":  version.Published,
			"prerelease": version.Prerelease,
		}

		// Evaluate the CEL expression using gomplate directly
		evaluated, err := gomplate.RunTemplate(data, gomplate.Template{
			Expression: expr,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate version_expr for version %s: %w", version.Version, err)
		}

		// Handle different return types from CEL expression
		switch evaluated {
		case "true":
			// Boolean true - include the version unchanged
			filteredVersions = append(filteredVersions, version)
		case "false":
			// Boolean false - exclude the version
			continue
		default:
			// Try to parse as a modified version object or keep as-is
			if shouldInclude, modified := parseExpressionResult(evaluated, version); shouldInclude {
				filteredVersions = append(filteredVersions, modified)
			}
		}
	}

	return filteredVersions, nil
}

// parseExpressionResult parses the result of a CEL expression evaluation
// Returns (shouldInclude, modifiedVersion)
func parseExpressionResult(evaluated string, original types.Version) (bool, types.Version) {
	// Handle boolean results (existing behavior)
	switch evaluated {
	case "true":
		return true, original
	case "false":
		return false, original
	}

	// Handle JSON object transformation (advanced)
	if isJSONObject(evaluated) {
		modified, err := parseVersionJSON(evaluated, original)
		if err != nil {
			// If JSON parsing fails, include original version
			return true, original
		}
		return true, modified
	}

	// Handle empty string (means exclude the version)
	if strings.TrimSpace(evaluated) == "" {
		return false, original
	}

	// Handle string transformation (new tag value)
	if isSimpleString(evaluated) {
		return true, transformTag(original, evaluated)
	}

	// Fallback: treat any other result as inclusion with original version
	return true, original
}

// Helper function to create version context for CEL evaluation
func createVersionContext(version types.Version) map[string]interface{} {
	return map[string]interface{}{
		"tag":        version.Tag,
		"version":    version.Version,
		"sha":        version.SHA,
		"published":  version.Published,
		"prerelease": version.Prerelease,
	}
}

// Common version filtering expressions for convenience
var CommonFilters = map[string]string{
	// Filtering expressions
	"no-prerelease":   "!prerelease",
	"only-prerelease": "prerelease",
	"v-prefix":        "tag.startsWith('v')",
	"no-v-prefix":     "!tag.startsWith('v')",
	"stable":          "!prerelease && !tag.contains('rc') && !tag.contains('beta') && !tag.contains('alpha')",
	"recent-year":     "published.getFullYear() >= (now().getFullYear() - 1)",

	// Transformation expressions using built-in CEL functions
	"remove-go-prefix": `tag.startsWith("go") ? tag.substring(2) : tag`,
	"remove-v-prefix":  `tag.startsWith("v") ? tag.substring(1) : tag`,
	"add-v-prefix":     `tag.startsWith("v") ? tag : "v" + tag`,
	"normalize-go":     `tag.startsWith("go") && tag.size() > 2 ? tag.substring(2) : (tag.startsWith("v") ? tag.substring(1) : tag)`,
}

// ApplyCommonFilter applies a predefined common filter
func ApplyCommonFilter(versions []types.Version, filterName string) ([]types.Version, error) {
	expr, exists := CommonFilters[filterName]
	if !exists {
		return nil, fmt.Errorf("unknown common filter: %s", filterName)
	}
	return ApplyVersionExpr(versions, expr)
}

// ValidateVersionExpr validates a version expression without applying it
func ValidateVersionExpr(expr string) error {
	if expr == "" {
		return nil
	}

	// Create a dummy version for validation
	testVersion := types.Version{
		Tag:        "v1.0.0",
		Version:    "1.0.0",
		SHA:        "abc123",
		Published:  time.Now(),
		Prerelease: false,
	}

	data := createVersionContext(testVersion)

	// Try to evaluate the expression
	_, err := gomplate.RunTemplate(data, gomplate.Template{
		Expression: expr,
	})
	if err != nil {
		return fmt.Errorf("invalid version_expr: %w", err)
	}

	return nil
}

// Helper functions for transformation support

// isJSONObject checks if a string appears to be a JSON object
func isJSONObject(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")
}

// isSimpleString checks if a string is a simple string transformation
// (not a boolean and not a JSON object)
func isSimpleString(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	if trimmed == "true" || trimmed == "false" {
		return false
	}
	if isJSONObject(trimmed) {
		return false
	}
	return true
}

// transformTag creates a new version with a transformed tag and version string
func transformTag(original types.Version, transformedValue string) types.Version {
	// Create a copy of the original version
	modified := original

	// Update both tag and version to the transformed value
	modified.Tag = transformedValue
	modified.Version = transformedValue

	return modified
}

// parseVersionJSON parses a JSON object to create a modified version
func parseVersionJSON(jsonStr string, original types.Version) (types.Version, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return original, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Start with the original version
	modified := original

	// Update fields if they exist in the JSON
	if tag, exists := data["tag"]; exists {
		if tagStr, ok := tag.(string); ok {
			modified.Tag = tagStr
		}
	}

	if version, exists := data["version"]; exists {
		if versionStr, ok := version.(string); ok {
			modified.Version = versionStr
		}
	}

	if sha, exists := data["sha"]; exists {
		if shaStr, ok := sha.(string); ok {
			modified.SHA = shaStr
		}
	}

	if prerelease, exists := data["prerelease"]; exists {
		if prereleaseFlag, ok := prerelease.(bool); ok {
			modified.Prerelease = prereleaseFlag
		}
	}

	// If include field is present and false, this acts as filtering
	if include, exists := data["include"]; exists {
		if includeFlag, ok := include.(bool); ok && !includeFlag {
			return original, fmt.Errorf("version excluded by include field")
		}
	}

	// Auto-normalize version if tag was changed but version wasn't explicitly set
	if _, tagExists := data["tag"]; tagExists {
		if _, versionExists := data["version"]; !versionExists {
			modified.Version = Normalize(modified.Tag)
		}
	}

	return modified, nil
}

// String transformation is handled by built-in CEL functions like:
// - tag.substring(n) - removes first n characters
// - tag.startsWith(prefix) - checks for prefix
// - tag.contains(substr) - checks for substring
// - tag + "suffix" - string concatenation
