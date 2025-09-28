package version

import (
	"fmt"
	"regexp"
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

// ExtractFromOutput extracts version using regex pattern from command output
// If no pattern provided, uses a default pattern for common version formats
func ExtractFromOutput(output, pattern string) (string, error) {
	if pattern == "" {
		// Default pattern for common version formats
		pattern = `v?(\d+(?:\.\d+)*(?:\.\d+)*(?:-[a-zA-Z0-9-_.]+)?)`
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid version pattern: %w", err)
	}

	matches := re.FindStringSubmatch(output)
	if len(matches) < 2 {
		return "", fmt.Errorf("version not found in output")
	}

	return Normalize(matches[1]), nil
}

// Compare compares two version strings, normalizing them first
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func Compare(v1, v2 string) (int, error) {
	// Normalize both versions
	norm1 := Normalize(v1)
	norm2 := Normalize(v2)

	// Handle exact string comparison first
	if norm1 == norm2 {
		return 0, nil
	}

	// Try semver comparison
	sv1, err1 := semver.NewVersion(norm1)
	sv2, err2 := semver.NewVersion(norm2)

	if err1 != nil || err2 != nil {
		// Fallback to string comparison if semver parsing fails
		if norm1 < norm2 {
			return -1, nil
		} else if norm1 > norm2 {
			return 1, nil
		}
		return 0, nil
	}

	return sv1.Compare(sv2), nil
}

// IsCompatible checks if installed version is compatible with required version
// Compatible means same major version and installed >= required
func IsCompatible(installed, required string) (bool, error) {
	norm1 := Normalize(installed)
	norm2 := Normalize(required)

	sv1, err1 := semver.NewVersion(norm1)
	sv2, err2 := semver.NewVersion(norm2)

	if err1 != nil || err2 != nil {
		// Fallback to exact match if semver parsing fails
		return norm1 == norm2, nil
	}

	// Same major version and installed >= required
	return sv1.Major() == sv2.Major() &&
		(sv1.GreaterThan(sv2) || sv1.Equal(sv2)), nil
}

// SatisfiesConstraint checks if version satisfies a semver constraint
func SatisfiesConstraint(version, constraint string) (bool, error) {
	normVersion := Normalize(version)

	// Handle special cases
	if constraint == "latest" {
		return true, nil
	}

	// Parse constraint
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		// If not a valid semver constraint, treat as exact version match
		return Normalize(constraint) == normVersion, nil
	}

	// Parse version
	v, err := semver.NewVersion(normVersion)
	if err != nil {
		return false, fmt.Errorf("invalid version %s: %w", normVersion, err)
	}

	return c.Check(v), nil
}

// GetNewerVersion returns the newer of two versions, or error if cannot compare
func GetNewerVersion(v1, v2 string) (string, error) {
	cmp, err := Compare(v1, v2)
	if err != nil {
		return "", err
	}

	if cmp >= 0 {
		return v1, nil
	}
	return v2, nil
}

// LooksLikeExactVersion checks if a string is an exact version vs a constraint
// Examples: "1.2.3", "v1.2.3" -> true
//
//	">=1.2.3", "~1.2.0", "latest" -> false
func LooksLikeExactVersion(s string) bool {
	if s == "" || s == "latest" {
		return false
	}

	// Check for constraint operators
	if strings.ContainsAny(s, ">=<~^*") {
		return false
	}

	// Remove 'v' prefix for checking
	normalized := Normalize(s)

	// Try parsing as exact semver - must have all three components
	if !IsPartialVersion(s) {
		_, err := semver.NewVersion(normalized)
		return err == nil
	}

	return false
}

// IsPartialVersion checks if a string is a partial version (major or major.minor)
// Examples: "1", "2", "1.5", "v2.1" -> true
//
//	"1.2.3", ">=1.2.3", "latest" -> false
func IsPartialVersion(s string) bool {
	if s == "" || s == "latest" {
		return false
	}

	// Check for constraint operators
	if strings.ContainsAny(s, ">=<~^*") {
		return false
	}

	// Remove 'v' prefix for checking
	normalized := Normalize(s)

	// Count dots to determine if it's partial
	dotCount := strings.Count(normalized, ".")

	// Must be major (0 dots) or major.minor (1 dot)
	if dotCount > 1 {
		return false
	}

	// Check if it's a valid version prefix
	// For major only: "2" should be valid
	// For major.minor: "1.5" should be valid
	if dotCount == 0 {
		// Major only - try parsing as "major.0.0"
		_, err := semver.NewVersion(normalized + ".0.0")
		return err == nil
	} else if dotCount == 1 {
		// Major.minor - try parsing as "major.minor.0"
		_, err := semver.NewVersion(normalized + ".0")
		return err == nil
	}

	return false
}
