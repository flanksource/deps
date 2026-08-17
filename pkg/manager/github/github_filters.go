package github

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/commons/collections"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

func releaseMatchesFilters(release restRelease, filters []string) bool {
	if len(filters) == 0 {
		return true
	}

	matchedPositive := false
	hasPositive := false
	for _, filter := range filters {
		if strings.HasPrefix(strings.TrimSpace(filter), "!") {
			if !collections.MatchItems(release.TagName, filter) || !collections.MatchItems(release.Name, filter) {
				return false
			}
			continue
		}

		hasPositive = true
		if collections.MatchItems(release.TagName, filter) || collections.MatchItems(release.Name, filter) {
			matchedPositive = true
		}
	}

	return !hasPositive || matchedPositive
}

func selectReleaseAsset(assets []restAsset, filters []string, sel manager.AssetSelection) *restAsset {
	if len(filters) == 0 {
		return nil
	}
	sel.IncludeNonBinary = sel.IncludeNonBinary || filtersTargetNonBinary(filters)

	if matched := matchReleaseAsset(assets, filters, sel); matched != nil {
		return matched
	}

	prefixFilters := make([]string, len(filters))
	for i, filter := range filters {
		trimmed := strings.TrimSpace(filter)
		if !strings.HasPrefix(trimmed, "!") && !strings.HasSuffix(trimmed, "*") {
			trimmed += "*"
		}
		prefixFilters[i] = trimmed
	}

	return matchReleaseAsset(assets, prefixFilters, sel)
}

// matchReleaseAsset returns the best asset matching the filters. Filters routinely match
// several assets - "tool-linux" widened to "tool-linux*" matches the binary, its checksum
// and its signature - so the survivors are ranked rather than taking the first one.
func matchReleaseAsset(assets []restAsset, filters []string, sel manager.AssetSelection) *restAsset {
	var candidates []restAsset
	for i := range assets {
		patterns := append([]string(nil), filters...)
		if collections.MatchItems(assets[i].Name, patterns...) {
			candidates = append(candidates, assets[i])
		}
	}
	return selectBestReleaseAsset(candidates, sel)
}

// selectBestReleaseAsset ranks candidates with manager.SelectBestAsset and maps the
// winner back to the original release asset, which carries the ID and digest.
func selectBestReleaseAsset(candidates []restAsset, sel manager.AssetSelection) *restAsset {
	if len(candidates) == 0 {
		return nil
	}

	infos := make([]manager.AssetInfo, len(candidates))
	for i, asset := range candidates {
		infos[i] = manager.AssetInfo{
			Name:        asset.Name,
			DownloadURL: asset.BrowserDownloadURL,
			SHA256:      stripChecksumPrefix(asset.Digest),
		}
	}

	best := manager.SelectBestAsset(infos, sel)
	if best == nil {
		return nil
	}

	if len(candidates) > 1 {
		logger.V(3).Infof("Selected asset %s from %d candidates: %v", best.Name, len(candidates), manager.AssetScores(infos, sel))
	}

	for i := range candidates {
		if candidates[i].Name == best.Name {
			return &candidates[i]
		}
	}
	return nil
}

// selectAssetByPattern returns the release asset for a templated asset pattern. An exact
// name match wins outright; a glob's matches are ranked, so a checksum file published
// alongside the binary can never be installed in its place.
func selectAssetByPattern(assets []restAsset, pattern string, sel manager.AssetSelection) *restAsset {
	for i := range assets {
		if assets[i].Name == pattern {
			return &assets[i]
		}
	}

	if !strings.Contains(pattern, "*") && !strings.Contains(pattern, "?") {
		return nil
	}
	sel.IncludeNonBinary = sel.IncludeNonBinary || manager.PatternTargetsNonBinary(pattern)

	var candidates []restAsset
	for i := range assets {
		if ok, _ := filepath.Match(pattern, assets[i].Name); ok {
			candidates = append(candidates, assets[i])
		}
	}
	return selectBestReleaseAsset(candidates, sel)
}

// filtersTargetNonBinary reports whether the user explicitly asked for a checksum or
// signature file, in which case it must not be filtered out of the candidate set.
func filtersTargetNonBinary(filters []string) bool {
	for _, filter := range filters {
		trimmed := strings.TrimSpace(filter)
		if !strings.HasPrefix(trimmed, "!") && manager.PatternTargetsNonBinary(trimmed) {
			return true
		}
	}
	return false
}

func filterReleaseCandidates(releases []restRelease, stableOnly bool, filters []string, limit int) []restRelease {
	filtered := make([]restRelease, 0, len(releases))
	for _, release := range releases {
		if release.Draft || (stableOnly && release.Prerelease) || !releaseMatchesFilters(release, filters) {
			continue
		}
		filtered = append(filtered, release)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered
}

func newAssetNotFoundError(pkg types.Package, tagName, pattern string, plat platform.Platform, assets []restAsset) error {
	availableAssetNames := make([]string, len(assets))
	for i, asset := range assets {
		availableAssetNames[i] = asset.Name
	}
	return manager.EnhanceAssetNotFoundError(pkg.Name, pattern, plat.String(), availableAssetNames,
		&manager.ErrAssetNotFound{
			Package:         fmt.Sprintf("%s@%s", pkg.Name, tagName),
			AssetPattern:    pattern,
			Platform:        plat.String(),
			AvailableAssets: availableAssetNames,
		})
}
