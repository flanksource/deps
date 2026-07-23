package github

import (
	"fmt"
	"strings"

	"github.com/flanksource/commons/collections"
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

func selectReleaseAsset(assets []restAsset, filters []string) *restAsset {
	if len(filters) == 0 {
		return nil
	}
	if matched := matchReleaseAsset(assets, filters); matched != nil {
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

	return matchReleaseAsset(assets, prefixFilters)
}

func matchReleaseAsset(assets []restAsset, filters []string) *restAsset {
	for i := range assets {
		patterns := append([]string(nil), filters...)
		if collections.MatchItems(assets[i].Name, patterns...) {
			return &assets[i]
		}
	}
	return nil
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
