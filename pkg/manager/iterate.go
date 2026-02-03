package manager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

// ReleaseInfo represents a release for iteration purposes
type ReleaseInfo struct {
	Tag         string
	Version     string
	PublishedAt time.Time
	IsPrerelease bool
}

// ReleaseIterator defines how to fetch and iterate releases
type ReleaseIterator interface {
	// FetchReleases fetches up to limit releases, ordered newest first
	FetchReleases(ctx context.Context, limit int) ([]ReleaseInfo, error)
	// TryResolve attempts to resolve a specific release to a download URL
	TryResolve(ctx context.Context, release ReleaseInfo) (*types.Resolution, error)
}

// IterateReleasesForAsset tries multiple releases until one has matching assets.
// Returns the first successful resolution or an enhanced error listing all tried versions.
func IterateReleasesForAsset(ctx context.Context, iterator ReleaseIterator, maxIterations int) (*types.Resolution, error) {
	releases, err := iterator.FetchReleases(ctx, maxIterations)
	if err != nil {
		return nil, err
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found")
	}

	var triedVersions []string
	var lastErr error

	for _, release := range releases {
		logger.V(3).Infof("Trying release %s (published: %s)", release.Tag, release.PublishedAt.Format("2006-01-02"))

		resolution, err := iterator.TryResolve(ctx, release)
		if err == nil {
			if len(triedVersions) > 0 {
				logger.Infof("Found assets in release %s after trying: %s", release.Tag, strings.Join(triedVersions, ", "))
			}
			return resolution, nil
		}

		// Only continue for asset-not-found errors
		if !IsAssetNotFoundError(err) {
			return nil, err
		}

		triedVersions = append(triedVersions, release.Tag)
		lastErr = err
		logger.V(3).Infof("Release %s has no matching assets, trying next...", release.Tag)
	}

	return nil, EnhanceIterationError(triedVersions, lastErr)
}

// IsAssetNotFoundError checks if an error is an asset-not-found error
func IsAssetNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var assetErr *ErrAssetNotFound
	return errors.As(err, &assetErr)
}

// EnhanceIterationError enhances an error with information about all tried releases
func EnhanceIterationError(triedVersions []string, lastErr error) error {
	if lastErr == nil {
		return fmt.Errorf("no matching assets found in %d releases: %s", len(triedVersions), strings.Join(triedVersions, ", "))
	}
	return fmt.Errorf("no matching assets found after trying %d releases (%s): %w",
		len(triedVersions), strings.Join(triedVersions, ", "), lastErr)
}

// FilterNonPrereleases filters out prerelease versions from a release list
func FilterNonPrereleases(releases []ReleaseInfo) []ReleaseInfo {
	var filtered []ReleaseInfo
	for _, r := range releases {
		if !r.IsPrerelease {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// IterationConfig holds configuration for release iteration
type IterationConfig struct {
	MaxIterations    int
	SkipPrereleases  bool
	Platform         platform.Platform
}

// DefaultIterationConfig returns sensible defaults for iteration
func DefaultIterationConfig(plat platform.Platform) IterationConfig {
	return IterationConfig{
		MaxIterations:   5,
		SkipPrereleases: true,
		Platform:        plat,
	}
}
