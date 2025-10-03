package manager

import (
	"strings"

	"github.com/flanksource/deps/pkg/platform"
)

// ParsePlatformEntry parses an entry that may have a platform prefix.
// Examples:
//   - "windows*: bin/tool.exe" → ("windows*", "bin/tool.exe", false, true)
//   - "!darwin-*: bin/tool" → ("darwin-*", "bin/tool", true, true)
//   - "bin/tool" → ("", "bin/tool", false, false)
func ParsePlatformEntry(entry string) (pattern string, value string, negated bool, hasPlatform bool) {
	entry = strings.TrimSpace(entry)

	// Check for colon separator
	colonIdx := strings.Index(entry, ":")
	if colonIdx == -1 {
		// No platform prefix - matches all platforms
		return "", entry, false, false
	}

	// Split into prefix and value
	prefix := strings.TrimSpace(entry[:colonIdx])
	value = strings.TrimSpace(entry[colonIdx+1:])

	// Check for negation
	if strings.HasPrefix(prefix, "!") {
		negated = true
		pattern = strings.TrimSpace(prefix[1:])
	} else {
		negated = false
		pattern = prefix
	}

	return pattern, value, negated, true
}

// MatchesPlatform checks if a platform matches the given pattern with negation support.
func MatchesPlatform(pattern string, plat platform.Platform, negated bool) bool {
	platformKey := plat.String() // e.g., "darwin-arm64"

	// Match using existing pattern matching logic
	matched := matchPlatformPattern(pattern, platformKey)

	// Apply negation if needed
	if negated {
		return !matched
	}
	return matched
}

// FilterEntriesByPlatform filters a list of entries based on platform patterns.
// Each entry can optionally have a platform prefix:
//   - "windows*: value" - only on Windows
//   - "!windows*: value" - all except Windows
//   - "value" - all platforms
//
// Returns a list of values (without platform prefixes) that match the given platform.
func FilterEntriesByPlatform(entries []string, plat platform.Platform) []string {
	var result []string

	for _, entry := range entries {
		pattern, value, negated, hasPlatform := ParsePlatformEntry(entry)

		// If no platform specified, include it
		if !hasPlatform {
			result = append(result, value)
			continue
		}

		// Check if platform matches
		if MatchesPlatform(pattern, plat, negated) {
			result = append(result, value)
		}
	}

	return result
}
