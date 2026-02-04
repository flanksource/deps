package manager

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/deps/pkg/platform"
)

// ResolveAssetPattern finds the best matching asset pattern for a platform.
// It follows this priority order:
// 1. Exact platform match (e.g., "darwin-arm64")
// 2. Literal "*" wildcard (all platforms)
// 3. Glob patterns (e.g., "darwin-*", "linux-*")
// 4. Comma-separated patterns (e.g., "darwin-*,windows-*")
func ResolveAssetPattern(assetPatterns map[string]string, plat platform.Platform, packageName ...string) (string, error) {
	if len(assetPatterns) == 0 {
		return "", fmt.Errorf("no asset patterns defined")
	}

	platformKey := plat.String() // e.g., "darwin-arm64"

	// 1. Try exact platform match first
	if pattern, exists := assetPatterns[platformKey]; exists {
		return pattern, nil
	}

	// 2. Try glob patterns like "darwin-*", "linux-*" (more specific than "*")
	for patternKey, patternValue := range assetPatterns {
		// Skip literal "*" here - it's handled later
		if patternKey != "*" && (strings.Contains(patternKey, "*") || strings.Contains(patternKey, ",")) {
			if matchPlatformPattern(patternKey, platformKey) {
				return patternValue, nil
			}
		}
	}

	// 3. Try literal "*" wildcard (all platforms) as last fallback
	if pattern, exists := assetPatterns["*"]; exists {
		return pattern, nil
	}

	// No match found - return structured error with available platforms
	availablePlatforms := make([]string, 0, len(assetPatterns))
	for k := range assetPatterns {
		availablePlatforms = append(availablePlatforms, k)
	}

	pkgName := ""
	if len(packageName) > 0 {
		pkgName = packageName[0]
	}
	return "", &ErrPlatformNotSupported{
		Package:            pkgName,
		Platform:           platformKey,
		AvailablePlatforms: availablePlatforms,
	}
}

// matchPlatformPattern checks if a platform matches a wildcard pattern.
// Supports comma-separated patterns like "darwin-*,windows-*"
func matchPlatformPattern(pattern string, platform string) bool {
	patterns := strings.Split(pattern, ",")
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if matched, _ := filepath.Match(p, platform); matched {
			return true
		}
	}
	return false
}

// NormalizeURLTemplate ensures that if a URL template ends with "/",
// it automatically includes the {{.asset}} placeholder.
// This allows for cleaner configuration where:
//
//	url_template: "https://example.com/files/"
//
// automatically becomes:
//
//	url_template: "https://example.com/files/{{.asset}}"
func NormalizeURLTemplate(urlTemplate string) string {
	if urlTemplate == "" {
		return urlTemplate
	}

	// Only append {{.asset}} if:
	// 1. URL ends with "/"
	// 2. {{.asset}} is not already present anywhere in the template
	if strings.HasSuffix(urlTemplate, "/") && !strings.Contains(urlTemplate, "{{.asset}}") {
		return urlTemplate + "{{.asset}}"
	}

	return urlTemplate
}
