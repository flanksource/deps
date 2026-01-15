package types

import (
	"sort"
	"strconv"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
)

// String returns the Tag if available, otherwise Version
func (v Version) String() string {
	if v.Tag != "" {
		return v.Tag
	}
	return v.Version
}

// IsSemver returns true if Major/Minor/Patch are populated
func (v Version) IsSemver() bool {
	return v.Major > 0 || v.Minor > 0 || v.Patch > 0
}

// IsPrerelease returns true if this is a prerelease version
func (v Version) IsPrerelease() bool {
	return v.Prerelease
}

// IsNightly returns true if this is a nightly/snapshot build
func (v Version) IsNightly() bool {
	lower := strings.ToLower(v.Version)
	return strings.Contains(lower, "nightly") || strings.Contains(lower, "snapshot")
}

// Compare returns -1 if v < other, 0 if equal, 1 if v > other
// Versions with dots sort before those without
// Uses numeric comparison of Major/Minor/Patch when available
func (v Version) Compare(other Version) int {
	v1HasDot := strings.Contains(v.Version, ".")
	v2HasDot := strings.Contains(other.Version, ".")

	// Versions with dots sort before those without
	if v1HasDot && !v2HasDot {
		return 1
	}
	if !v1HasDot && v2HasDot {
		return -1
	}

	// If both have semver parsed, use numeric comparison
	if v.IsSemver() && other.IsSemver() {
		if v.Major != other.Major {
			if v.Major > other.Major {
				return 1
			}
			return -1
		}
		if v.Minor != other.Minor {
			if v.Minor > other.Minor {
				return 1
			}
			return -1
		}
		if v.Patch != other.Patch {
			if v.Patch > other.Patch {
				return 1
			}
			return -1
		}
		return 0
	}

	// Fallback: numeric comparison of dot-separated parts
	return compareVersionStrings(v.Version, other.Version)
}

// compareVersionStrings compares two version strings numerically.
func compareVersionStrings(v1, v2 string) int {
	// Strip build metadata (+...) for comparison
	if idx := strings.Index(v1, "+"); idx != -1 {
		v1 = v1[:idx]
	}
	if idx := strings.Index(v2, "+"); idx != -1 {
		v2 = v2[:idx]
	}

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			n1, _ = strconv.Atoi(parts1[i])
		}
		if i < len(parts2) {
			n2, _ = strconv.Atoi(parts2[i])
		}

		if n1 != n2 {
			if n1 > n2 {
				return 1
			}
			return -1
		}
	}

	return 0
}

// Pretty returns a rich text representation of the version
func (v Version) Pretty() api.Text {
	text := clicky.Text("")

	if v.Prerelease {
		text.Add(icons.Warning).Append(" ")
	}

	text.Append(v.String())

	if v.Prerelease {
		text.Append(" (prerelease)", "text-yellow-500")
	}

	if !v.Published.IsZero() {
		text.Append(" " + v.Published.Format("2006-01-02"), "text-gray-500")
	}

	return text
}

// Versions is a slice of Version with sorting methods
type Versions []Version

// Sort sorts versions in descending order (newest first)
func (v Versions) Sort() {
	sort.Slice(v, func(i, j int) bool {
		return v[i].Compare(v[j]) > 0
	})
}

// Latest returns the latest stable version, or latest prerelease if no stable
func (v Versions) Latest() *Version {
	if len(v) == 0 {
		return nil
	}

	// First try to find latest stable
	if stable := v.LatestStable(); stable != nil {
		return stable
	}

	// Return first version (should be latest after sorting)
	return &v[0]
}

// LatestStable returns the latest non-prerelease version
func (v Versions) LatestStable() *Version {
	for i := range v {
		if !v[i].Prerelease {
			return &v[i]
		}
	}
	return nil
}

// ParseVersion parses a version string and returns a Version with Major/Minor/Patch populated
func ParseVersion(versionStr, tag string) Version {
	v := Version{
		Version: versionStr,
		Tag:     tag,
	}

	// Strip build metadata (+...) before parsing
	parseStr := versionStr
	if idx := strings.Index(parseStr, "+"); idx != -1 {
		parseStr = parseStr[:idx]
	}

	// Strip leading 'v' if present
	parseStr = strings.TrimPrefix(parseStr, "v")

	// Split by dots
	parts := strings.Split(parseStr, ".")
	if len(parts) >= 1 {
		v.Major, _ = strconv.ParseInt(parts[0], 10, 64)
	}
	if len(parts) >= 2 {
		v.Minor, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if len(parts) >= 3 {
		v.Patch, _ = strconv.ParseInt(parts[2], 10, 64)
	}

	// Detect prerelease
	v.Prerelease = isPrerelease(versionStr)

	return v
}

// isPrerelease checks if a version string indicates a prerelease
func isPrerelease(version string) bool {
	lower := strings.ToLower(version)
	prereleaseParts := []string{"alpha", "beta", "rc", "pre", "dev", "nightly", "snapshot", "-ea"}
	for _, part := range prereleaseParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}
