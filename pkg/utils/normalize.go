package utils

import "strings"

// Normalize removes common prefixes and suffixes from version strings
// Handles: v1.2.3 -> 1.2.3, release-1.2.3 -> 1.2.3, version-1.2.3 -> 1.2.3, jq-1.7 -> 1.7
func Normalize(version string) string {
	if version == "" {
		return version
	}

	// Trim whitespace first
	version = strings.TrimSpace(version)

	// Remove common prefixes (check longer prefixes first)
	version = strings.TrimPrefix(version, "version-")
	version = strings.TrimPrefix(version, "Version-")
	version = strings.TrimPrefix(version, "release-")
	version = strings.TrimPrefix(version, "Release-")
	version = strings.TrimPrefix(version, "v")
	version = strings.TrimPrefix(version, "V")

	// Strip package name prefix if present (e.g., "jq-1.7" -> "1.7")
	// Pattern: {word}-{version} or {word}_{version}
	// Only strip if what follows looks like a version
	if idx := strings.IndexAny(version, "-_"); idx > 0 {
		possibleVersion := version[idx+1:]
		// Check if the part after separator looks like a version
		if looksLikeVersion(possibleVersion) {
			version = possibleVersion
		}
	}

	// Remove common suffixes
	version = strings.TrimSuffix(version, "-release")
	version = strings.TrimSuffix(version, "-Release")

	return version
}

// looksLikeVersion checks if a string looks like a version number
// Must start with a digit or 'v'/'V' followed by digit
func looksLikeVersion(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Starts with digit
	if s[0] >= '0' && s[0] <= '9' {
		return true
	}
	// Starts with 'v' or 'V' followed by digit
	if len(s) > 1 && (s[0] == 'v' || s[0] == 'V') && s[1] >= '0' && s[1] <= '9' {
		return true
	}
	return false
}
