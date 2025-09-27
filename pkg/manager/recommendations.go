package manager

import (
	"fmt"
	"path/filepath"
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

// EnhanceAssetNotFoundError enhances an asset not found error with available asset information
func EnhanceAssetNotFoundError(packageName, assetPattern, platform string, availableAssets []string, originalErr error) error {
	if len(availableAssets) == 0 {
		return fmt.Errorf("%w\n\nNo assets found for %s", originalErr, packageName)
	}

	// Build enhanced error message
	var errorMsg strings.Builder
	errorMsg.WriteString(fmt.Sprintf("Asset not found: %s for %s in package %s\n\n", assetPattern, platform, packageName))
	errorMsg.WriteString(fmt.Sprintf("Available assets (%d total):\n", len(availableAssets)))

	// Show up to 20 assets as requested
	maxAssets := 20
	displayAssets := availableAssets
	if len(displayAssets) > maxAssets {
		displayAssets = displayAssets[:maxAssets]
	}

	for _, asset := range displayAssets {
		errorMsg.WriteString(fmt.Sprintf("  %s\n", asset))
	}

	if len(availableAssets) > maxAssets {
		errorMsg.WriteString(fmt.Sprintf("  ... and %d more assets\n", len(availableAssets)-maxAssets))
	}

	// Show the pattern that was searched for
	errorMsg.WriteString(fmt.Sprintf("\nSearched for pattern: %s", assetPattern))

	// Suggest closest match
	if suggestion := SuggestClosestAsset(assetPattern, availableAssets); suggestion != "" {
		errorMsg.WriteString(fmt.Sprintf("\nDid you mean: %s?", suggestion))
	}

	return fmt.Errorf("%s", errorMsg.String())
}

// SuggestClosestAsset finds the closest matching asset from available assets
func SuggestClosestAsset(targetAsset string, availableAssets []string) string {
	if len(availableAssets) == 0 {
		return ""
	}

	var bestMatch string
	bestScore := 0

	for _, asset := range availableAssets {
		score := calculateAssetSimilarity(targetAsset, asset)
		if score > bestScore {
			bestScore = score
			bestMatch = asset
		}
	}

	// Only suggest if similarity is reasonably high (at least 30% match)
	if bestScore > 30 {
		return bestMatch
	}

	return ""
}

// calculateAssetSimilarity calculates similarity between two asset names
// Returns a score from 0-100 based on common substrings and patterns
func calculateAssetSimilarity(target, candidate string) int {
	if target == candidate {
		return 100
	}

	// Convert to lowercase for comparison
	target = strings.ToLower(target)
	candidate = strings.ToLower(candidate)

	// Check if candidate contains target as substring
	if strings.Contains(candidate, target) {
		return 80
	}
	if strings.Contains(target, candidate) {
		return 80
	}

	// Check for common file extensions and patterns
	targetBase := strings.TrimSuffix(target, filepath.Ext(target))
	candidateBase := strings.TrimSuffix(candidate, filepath.Ext(candidate))

	if targetBase == candidateBase {
		return 70
	}

	// Split by common separators and check overlap
	targetParts := splitAssetName(target)
	candidateParts := splitAssetName(candidate)

	commonParts := 0
	for _, tPart := range targetParts {
		for _, cPart := range candidateParts {
			if tPart == cPart {
				commonParts++
				break
			}
		}
	}

	totalParts := len(targetParts) + len(candidateParts)
	if totalParts == 0 {
		return 0
	}

	// Calculate similarity based on common parts
	similarity := (commonParts * 2 * 100) / totalParts
	return similarity
}

// splitAssetName splits an asset name into meaningful parts
func splitAssetName(assetName string) []string {
	// Remove file extensions
	base := strings.TrimSuffix(assetName, filepath.Ext(assetName))

	// Split by common separators
	separators := []string{"-", "_", "."}
	parts := []string{base}

	for _, sep := range separators {
		var newParts []string
		for _, part := range parts {
			newParts = append(newParts, strings.Split(part, sep)...)
		}
		parts = newParts
	}

	// Filter out empty parts
	var result []string
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}