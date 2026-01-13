package manager

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/agnivade/levenshtein"
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
	minDiff := ^uint64(0) // Max uint64

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

	// Extract OS from platform string (e.g., "darwin-arm64" -> "darwin")
	targetOS := ""
	if parts := strings.Split(platform, "-"); len(parts) > 0 {
		targetOS = strings.ToLower(parts[0])
	}

	// Filter out non-binary files first (.sig, .json, .txt, .msi for non-Windows)
	filteredAssets := FilterNonBinaryAssetNames(availableAssets, targetOS)

	// Then filter by target OS and architecture
	filteredAssets = filterAssetsByTargetOS(filteredAssets, platform)
	filteredAssets = filterAssetsByTargetArch(filteredAssets, platform)

	// Sort assets by Levenshtein distance (most similar first) and calculate distances
	type assetWithDistance struct {
		name     string
		distance int
		score    int
	}

	assetsWithDist := make([]assetWithDistance, len(filteredAssets))
	for i, asset := range filteredAssets {
		dist := levenshtein.ComputeDistance(strings.ToLower(assetPattern), strings.ToLower(asset))
		score := calculateAssetSimilarity(assetPattern, asset)
		assetsWithDist[i] = assetWithDistance{
			name:     asset,
			distance: dist,
			score:    score,
		}
	}

	sort.SliceStable(assetsWithDist, func(i, j int) bool {
		return assetsWithDist[i].distance < assetsWithDist[j].distance
	})

	// Build enhanced error message
	var errorMsg strings.Builder
	errorMsg.WriteString(fmt.Sprintf("Asset not found: %s for %s in package %s\n\n", assetPattern, platform, packageName))
	errorMsg.WriteString(fmt.Sprintf("Available assets (%d total):\n", len(assetsWithDist)))

	// Show up to 20 assets (most relevant ones)
	maxAssets := 20
	displayAssets := assetsWithDist
	if len(displayAssets) > maxAssets {
		displayAssets = displayAssets[:maxAssets]
	}

	for _, asset := range displayAssets {
		errorMsg.WriteString(fmt.Sprintf("  %s\n", asset.name))
	}

	if len(assetsWithDist) > maxAssets {
		errorMsg.WriteString(fmt.Sprintf("  ... and %d more assets\n", len(assetsWithDist)-maxAssets))
	}

	// Show the pattern that was searched for
	errorMsg.WriteString(fmt.Sprintf("\nSearched for pattern: %s", assetPattern))

	// Suggest closest match (first sorted asset)
	sortedAssetNames := make([]string, len(assetsWithDist))
	for i, a := range assetsWithDist {
		sortedAssetNames[i] = a.name
	}
	if suggestion := SuggestClosestAsset(assetPattern, sortedAssetNames); suggestion != "" {
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

// filterAssetsByTargetOS filters assets to exclude those from other OSes
func filterAssetsByTargetOS(assets []string, platform string) []string {
	parts := strings.Split(platform, "-")
	if len(parts) == 0 {
		return assets
	}
	targetOS := strings.ToLower(parts[0])

	// OS patterns with word boundary awareness
	// Each key is the canonical OS, values are patterns that identify it
	osPatterns := map[string][]string{
		"darwin":  {"darwin", "macos", "osx"},         // "mac" is too short and ambiguous
		"linux":   {"linux"},
		"windows": {"windows", "win32", "win64"},      // "win" alone matches "darwin"
	}

	// Also add "-mac-" and "-win-" patterns for word-boundary matching
	wordBoundaryPatterns := map[string][]string{
		"darwin":  {"-mac-", "_mac_", "-mac.", "_mac."},
		"windows": {"-win-", "_win_", "-win.", "_win."},
	}

	// Check if an asset matches a specific OS
	matchesOS := func(assetLower string, osKey string) bool {
		// Check exact patterns
		for _, pattern := range osPatterns[osKey] {
			if strings.Contains(assetLower, pattern) {
				return true
			}
		}
		// Check word-boundary patterns
		for _, pattern := range wordBoundaryPatterns[osKey] {
			if strings.Contains(assetLower, pattern) {
				return true
			}
		}
		return false
	}

	var filtered []string
	for _, asset := range assets {
		assetLower := strings.ToLower(asset)
		exclude := false
		for os := range osPatterns {
			if os != targetOS && matchesOS(assetLower, os) {
				exclude = true
				break
			}
		}
		if !exclude {
			filtered = append(filtered, asset)
		}
	}

	if len(filtered) == 0 {
		return assets
	}
	return filtered
}

// filterAssetsByTargetArch filters assets to exclude those from other architectures
func filterAssetsByTargetArch(assets []string, platform string) []string {
	parts := strings.Split(platform, "-")
	if len(parts) < 2 {
		return assets
	}
	targetArch := strings.ToLower(parts[1])

	// Architecture patterns - each key is a canonical arch, values identify assets for that arch
	// Order matters for matching - check longer patterns first to avoid false positives
	archPatterns := map[string][]string{
		"amd64":   {"amd64", "x86_64", "x86-64"},
		"arm64":   {"arm64", "aarch64"},
		"arm":     {"armv7", "armv7l", "armv6"},
		"386":     {"i386", "i686"},
		"ppc64le": {"ppc64le"},
		"ppc64":   {"ppc64"},
		"s390x":   {"s390x"},
		"riscv64": {"riscv64"},
	}

	// Word-boundary patterns for short arch names that could match falsely
	wordBoundaryPatterns := map[string][]string{
		"arm":   {"_arm_", "-arm-", "_arm.", "-arm."},
		"amd64": {"_x64_", "-x64-", "_x64.", "-x64."},
		"386":   {"_x86_", "-x86-", "_x86.", "-x86.", "_386_", "-386-", "_386.", "-386."},
	}

	matchesArch := func(assetLower string, archKey string) bool {
		for _, pattern := range archPatterns[archKey] {
			if strings.Contains(assetLower, pattern) {
				return true
			}
		}
		for _, pattern := range wordBoundaryPatterns[archKey] {
			if strings.Contains(assetLower, pattern) {
				return true
			}
		}
		return false
	}

	var filtered []string
	for _, asset := range assets {
		assetLower := strings.ToLower(asset)
		exclude := false
		for arch := range archPatterns {
			if arch != targetArch && matchesArch(assetLower, arch) {
				exclude = true
				break
			}
		}
		if !exclude {
			filtered = append(filtered, asset)
		}
	}

	if len(filtered) == 0 {
		return assets
	}
	return filtered
}

// calculateAssetSimilarity calculates similarity between two asset names using Levenshtein distance
// Returns a score from 0-100 where 100 is exact match and 0 is very dissimilar
func calculateAssetSimilarity(target, candidate string) int {
	if target == candidate {
		return 100
	}

	// Convert to lowercase for comparison
	targetLower := strings.ToLower(target)
	candidateLower := strings.ToLower(candidate)

	// Calculate Levenshtein distance
	distance := levenshtein.ComputeDistance(targetLower, candidateLower)

	// Convert distance to similarity score (0-100)
	// Use the length of the longer string as the maximum possible distance
	maxLen := len(targetLower)
	if len(candidateLower) > maxLen {
		maxLen = len(candidateLower)
	}

	if maxLen == 0 {
		return 100
	}

	// Calculate similarity percentage: 100% - (distance/maxLen * 100)
	score := 100 - (distance*100)/maxLen
	if score < 0 {
		score = 0
	}

	return score
}
