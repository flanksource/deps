package version

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

// PackageManager defines the minimal interface needed for version resolution
type PackageManager interface {
	// Name returns the manager type identifier
	Name() string

	// DiscoverVersions returns the most recent versions for a package
	// limit=0 means return all versions, limit>0 means return at most that many
	// Results should be ordered with newest versions first
	DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error)
}

// VersionResolver handles centralized version constraint resolution
type VersionResolver struct {
	mgr PackageManager
}

// NewResolver creates a new version resolver for the given package manager
func NewResolver(mgr PackageManager) *VersionResolver {
	return &VersionResolver{mgr: mgr}
}

// ResolveConstraint resolves a version constraint to a specific version
// Handles "latest", exact versions, and semver constraints uniformly across all managers
func (r *VersionResolver) ResolveConstraint(ctx context.Context, pkg types.Package, constraint string, plat platform.Platform) (string, error) {
	if constraint == "" {
		return "", fmt.Errorf("empty version constraint")
	}

	// Determine optimal limit based on constraint type
	limit := r.getOptimalLimit(constraint)

	// Get versions from the package manager
	versions, err := r.mgr.DiscoverVersions(ctx, pkg, plat, limit)
	if err != nil {
		return "", fmt.Errorf("failed to discover versions: %w", err)
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no versions found")
	}

	// Try to resolve with the limited set
	resolved, err := r.selectBestVersion(pkg, versions, constraint)

	// If we couldn't resolve and we used a limit, try with more versions
	if err != nil && limit > 0 {
		if needsMoreVersions(err, constraint) {
			// Try with a larger limit
			newLimit := limit * 5
			if newLimit > 1000 {
				newLimit = 0 // Get all versions
			}

			moreVersions, err2 := r.mgr.DiscoverVersions(ctx, pkg, plat, newLimit)
			if err2 != nil {
				return "", fmt.Errorf("failed to discover more versions: %w", err2)
			}

			if len(moreVersions) > len(versions) {
				resolved, err = r.selectBestVersion(pkg, moreVersions, constraint)
			}
		}
	}

	return resolved, err
}

// getOptimalLimit returns the optimal number of versions to fetch for a given constraint
func (r *VersionResolver) getOptimalLimit(constraint string) int {
	switch constraint {
	case "latest":
		return 10 // Only need most recent stable version
	case "stable":
		return 20 // Need to find latest non-prerelease
	default:
		if LooksLikeExactVersion(constraint) {
			return 200 // May need to search deeper for exact versions
		}

		// Check if it's a semver constraint
		if _, err := ParseConstraint(constraint); err == nil {
			// For semver ranges, start with a reasonable limit
			if isNarrowConstraint(constraint) {
				return 50 // ~1.2.0, =1.2.3
			}
			return 100 // ^1.0.0, >=1.0.0
		}

		return 50 // Default reasonable limit
	}
}

// selectBestVersion selects the best version from available versions based on constraint
func (r *VersionResolver) selectBestVersion(pkg types.Package, versions []types.Version, constraint string) (string, error) {
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions available")
	}

	// Handle special constraints
	switch constraint {
	case "latest":
		return r.getLatestVersion(versions, false), nil // Include prereleases if no stable
	case "stable":
		latest := r.getLatestVersion(versions, true) // Stable only
		if latest == "" {
			return "", fmt.Errorf("no stable versions found")
		}
		return latest, nil
	}

	// Handle exact versions
	if LooksLikeExactVersion(constraint) {
		return r.findExactVersion(pkg, versions, constraint)
	}

	// Handle partial versions
	if IsPartialVersion(constraint) {
		return r.findLatestInRange(pkg, versions, constraint)
	}

	// Handle semver constraints
	constraintFilter, err := ParseConstraint(constraint)
	if err != nil {
		// If not a valid constraint, treat as exact version
		return r.findExactVersion(pkg, versions, constraint)
	}

	// Find the highest version that satisfies the constraint
	var candidates []types.Version
	for _, v := range versions {
		if constraintFilter.Check(v.Version) {
			candidates = append(candidates, v)
		}
	}

	if len(candidates) == 0 {
		originalErr := fmt.Errorf("no versions satisfy constraint %s", constraint)
		return "", r.enhanceVersionError(pkg.Name, constraint, versions, originalErr)
	}

	// Sort candidates by version (newest first) and return the best
	sort.Slice(candidates, func(i, j int) bool {
		cmp, err := Compare(candidates[i].Version, candidates[j].Version)
		if err != nil {
			return false
		}
		return cmp > 0 // Higher version comes first
	})

	return candidates[0].Tag, nil // Return original tag, not normalized version
}

// getLatestVersion returns the latest version, optionally excluding prereleases
func (r *VersionResolver) getLatestVersion(versions []types.Version, stableOnly bool) string {
	for _, v := range versions {
		if stableOnly && v.Prerelease {
			continue
		}
		return v.Tag // Return original tag format
	}

	// If no stable versions found and stable required, return empty
	if stableOnly {
		return ""
	}

	// Return latest version even if prerelease
	if len(versions) > 0 {
		return versions[0].Tag
	}

	return ""
}

// findExactVersion finds an exact version match, handling v prefix variations
func (r *VersionResolver) findExactVersion(pkg types.Package, versions []types.Version, target string) (string, error) {
	normalizedTarget := Normalize(target)

	for _, v := range versions {
		if v.Version == normalizedTarget || v.Tag == target {
			return v.Tag, nil // Return original tag format
		}
	}

	// Enhance error with version recommendations
	originalErr := fmt.Errorf("version %s not found", target)
	return "", r.enhanceVersionError(pkg.Name, target, versions, originalErr)
}

// findLatestInRange finds the latest version that matches a partial version pattern
func (r *VersionResolver) findLatestInRange(pkg types.Package, versions []types.Version, pattern string) (string, error) {

	// Create a constraint for the partial version
	constraint, err := ParseConstraint(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid partial version pattern %s: %w", pattern, err)
	}

	// Find all versions that match the pattern
	var candidates []types.Version
	for _, v := range versions {
		// Skip prereleases unless specifically requested
		if v.Prerelease {
			continue
		}

		if constraint.Check(v.Version) {
			candidates = append(candidates, v)
		}
	}

	if len(candidates) == 0 {
		// Try including prereleases if no stable versions found
		for _, v := range versions {
			if constraint.Check(v.Version) {
				candidates = append(candidates, v)
			}
		}

		if len(candidates) == 0 {
			return "", fmt.Errorf("no versions found matching pattern %s", pattern)
		}
	}

	// Sort candidates by version (newest first) and return the best
	sort.Slice(candidates, func(i, j int) bool {
		cmp, err := Compare(candidates[i].Version, candidates[j].Version)
		if err != nil {
			return false
		}
		return cmp > 0 // Higher version comes first
	})

	return candidates[0].Tag, nil // Return original tag, not normalized version
}

// needsMoreVersions determines if we should fetch more versions based on the error
func needsMoreVersions(err error, constraint string) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// If we couldn't find an exact version or satisfy a constraint,
	// it might be in older versions
	return contains(errStr, "not found") ||
		contains(errStr, "no versions satisfy") ||
		contains(errStr, "no stable versions")
}

// isNarrowConstraint checks if a constraint is narrow (likely to match recent versions)
func isNarrowConstraint(constraint string) bool {
	// Narrow constraints that likely match recent versions
	return contains(constraint, "~") || // ~1.2.0 (patch-level changes)
		(contains(constraint, "=") && !contains(constraint, ">=") && !contains(constraint, "<=")) || // =1.2.3 (exact)
		(contains(constraint, ">=") && contains(constraint, "<")) // range with upper bound
}

// contains is a simple string contains check
func contains(s, substr string) bool {
	return len(substr) > 0 && len(s) >= len(substr) &&
		findSubstring(s, substr) != -1
}

// findSubstring finds the index of substr in s, returns -1 if not found
func findSubstring(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(substr) > len(s) {
		return -1
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// enhanceVersionError enhances version errors with available version information
func (r *VersionResolver) enhanceVersionError(packageName, requestedVersion string, availableVersions []types.Version, originalErr error) error {
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
	if suggestion := r.suggestClosestVersion(requestedVersion, availableVersions); suggestion != "" {
		errorMsg.WriteString(fmt.Sprintf("\nDid you mean: %s?", suggestion))
	}

	return fmt.Errorf("%s", errorMsg.String())
}

// suggestClosestVersion finds the closest matching version from available versions
func (r *VersionResolver) suggestClosestVersion(requestedVersion string, availableVersions []types.Version) string {
	if len(availableVersions) == 0 {
		return ""
	}

	// If requested version looks like a semver, try to find closest match
	requestedSemver, err := semver.NewVersion(Normalize(requestedVersion))
	if err != nil {
		// If not semver, suggest latest stable
		return r.getLatestStableVersion(availableVersions)
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
		diff := r.calculateVersionDiff(requestedSemver, availableSemver)
		if diff < minDiff {
			minDiff = diff
			closest = v
		}
	}

	if closest != nil {
		return closest.Tag
	}

	// Fallback to latest stable
	return r.getLatestStableVersion(availableVersions)
}

// getLatestStableVersion returns the latest stable version from available versions
func (r *VersionResolver) getLatestStableVersion(availableVersions []types.Version) string {
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
func (r *VersionResolver) calculateVersionDiff(v1, v2 *semver.Version) uint64 {
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
