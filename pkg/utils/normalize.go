package utils

import (
	"strings"

	"github.com/Masterminds/semver/v3"
)

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

	// Remove common suffixes before semver validation
	// These are not valid semver prereleases, just naming conventions
	version = strings.TrimSuffix(version, "-release")
	version = strings.TrimSuffix(version, "-Release")

	// If current version is valid semver, don't strip further
	if _, err := semver.NewVersion(version); err == nil {
		return version
	}

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

	return version
}

// looksLikeVersion checks if a string looks like a semantic version number
// Must match pattern: MAJOR.MINOR (e.g., "1.2", "1.2.3", "v1.2.3")
// This prevents date-based strings like "2024-02-07" or "12-15.1" from being treated as versions
func looksLikeVersion(s string) bool {
	if len(s) == 0 {
		return false
	}

	// Strip optional v/V prefix for checking
	check := s
	if len(check) > 1 && (check[0] == 'v' || check[0] == 'V') {
		check = check[1:]
	}

	// Must start with digit
	if len(check) == 0 || check[0] < '0' || check[0] > '9' {
		return false
	}

	// Must contain a dot (MAJOR.MINOR pattern) to distinguish from dates
	// e.g., "1.2.3" is a version, "2024-02-07" is not
	dotIdx := strings.Index(check, ".")
	if dotIdx < 1 {
		return false
	}

	// Character after dot must be a digit
	if dotIdx+1 >= len(check) || check[dotIdx+1] < '0' || check[dotIdx+1] > '9' {
		return false
	}

	// Reject date-like patterns where the first component contains a dash
	// e.g., "12-15.1" is likely a date fragment, not a version
	dashIdx := strings.Index(check, "-")
	if dashIdx > 0 && dashIdx < dotIdx {
		// The first component has a dash before the dot - likely a date fragment
		return false
	}

	// Reject date-like patterns: YYYY-MM-DD or similar (4 digits followed by dash)
	// e.g., "2010-12-15.1" should not be treated as a version
	if len(check) >= 4 && check[0] >= '1' && check[0] <= '2' {
		allDigits := true
		for i := 0; i < 4 && i < len(check); i++ {
			if check[i] < '0' || check[i] > '9' {
				allDigits = false
				break
			}
		}
		if allDigits && len(check) > 4 && check[4] == '-' {
			return false
		}
	}

	return true
}
