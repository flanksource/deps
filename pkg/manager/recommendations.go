package manager

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
)

// EnhanceErrorWithVersions enhances an error with available version information
func EnhanceErrorWithVersions(packageName, requestedVersion string, availableVersions []types.Version, originalErr error) error {
	if len(availableVersions) == 0 {
		return fmt.Errorf("%w\n\nNo versions found for %s", originalErr, packageName)
	}

	// Build enhanced error message
	var errorMsg strings.Builder
	errorMsg.WriteString(fmt.Sprintf("Version %s not found for %s\n\n", requestedVersion, packageName))
	errorMsg.WriteString(fmt.Sprintf("Available versions (%d total):\n", len(availableVersions)))

	// Show latest versions (up to 10)
	maxVersions := 10
	displayVersions := availableVersions
	if len(displayVersions) > maxVersions {
		displayVersions = displayVersions[:maxVersions]
	}

	for _, v := range displayVersions {
		suffix := ""
		if v.Prerelease {
			suffix += " (prerelease)"
		}
		errorMsg.WriteString(fmt.Sprintf("  %s%s\n", v.Tag, suffix))
	}

	if len(availableVersions) > maxVersions {
		errorMsg.WriteString(fmt.Sprintf("  ... and %d more versions\n", len(availableVersions)-maxVersions))
	}

	// Suggest closest match
	if suggestion := SuggestClosestVersion(requestedVersion, availableVersions); suggestion != "" {
		errorMsg.WriteString(fmt.Sprintf("\nDid you mean: %s?", suggestion))
	}

	return fmt.Errorf("%s", errorMsg.String())
}

// SuggestClosestVersion finds the closest matching version from available versions
func SuggestClosestVersion(requestedVersion string, availableVersions []types.Version) string {
	if len(availableVersions) == 0 {
		return ""
	}

	// If requested version looks like a semver, try to find closest match
	requestedSemver, err := semver.NewVersion(version.Normalize(requestedVersion))
	if err != nil {
		// If not semver, suggest latest stable
		return getLatestStable(availableVersions)
	}

	var closest *types.Version
	var minDiff uint64 = ^uint64(0) // Max uint64

	for i := range availableVersions {
		v := &availableVersions[i]
		if v.Prerelease {
			continue // Skip prereleases for suggestions
		}

		availableSemver, err := semver.NewVersion(v.Version)
		if err != nil {
			continue
		}

		// Calculate difference
		diff := calculateVersionDiff(requestedSemver, availableSemver)
		if diff < minDiff {
			minDiff = diff
			closest = v
		}
	}

	if closest != nil {
		return closest.Tag
	}

	// Fallback to latest stable
	return getLatestStable(availableVersions)
}

// getLatestStable returns the latest stable version from available versions
func getLatestStable(availableVersions []types.Version) string {
	var latestStable *types.Version
	var latestSemver *semver.Version

	for i := range availableVersions {
		v := &availableVersions[i]
		if v.Prerelease {
			continue
		}

		vSemver, err := semver.NewVersion(v.Version)
		if err != nil {
			// If we can't parse as semver, use first non-prerelease
			if latestStable == nil {
				latestStable = v
			}
			continue
		}

		if latestSemver == nil || vSemver.GreaterThan(latestSemver) {
			latestSemver = vSemver
			latestStable = v
		}
	}

	if latestStable != nil {
		return latestStable.Tag
	}

	// If no stable versions, return first available
	return availableVersions[0].Tag
}

// calculateVersionDiff calculates the "distance" between two semantic versions
func calculateVersionDiff(v1, v2 *semver.Version) uint64 {
	majorDiff := uint64(abs(int(v1.Major()) - int(v2.Major())))
	minorDiff := uint64(abs(int(v1.Minor()) - int(v2.Minor())))
	patchDiff := uint64(abs(int(v1.Patch()) - int(v2.Patch())))

	// For major version differences > 0, suggest the latest stable instead
	// since it's likely they want the newest available
	if majorDiff > 0 {
		return ^uint64(0) // Max value to de-prioritize
	}

	// Weight minor and patch differences
	return minorDiff*1000 + patchDiff
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}