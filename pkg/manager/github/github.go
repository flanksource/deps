package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/checksum"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	depstemplate "github.com/flanksource/deps/pkg/template"
	"github.com/flanksource/deps/pkg/types"
	versionpkg "github.com/flanksource/deps/pkg/version"
	"github.com/google/go-github/v57/github"
	"github.com/shurcooL/githubv4"
)

// GitHubReleaseManager implements the PackageManager interface for GitHub releases
type GitHubReleaseManager struct {
	// Uses shared singleton GitHub client
}

// NewGitHubReleaseManager creates a new GitHub release manager.
func NewGitHubReleaseManager() *GitHubReleaseManager {
	return &GitHubReleaseManager{}
}

// GraphQL query structs for releases

// releasesQuery fetches releases without assets (for version discovery)
type releasesQuery struct {
	Repository struct {
		Releases struct {
			Nodes []struct {
				Name         string
				TagName      string
				PublishedAt  time.Time
				IsPrerelease bool
				TagCommit    struct {
					Oid string
				}
			}
			PageInfo struct {
				HasNextPage bool
				EndCursor   githubv4.String
			}
		} `graphql:"releases(first: $first, orderBy: {field: CREATED_AT, direction: DESC})"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// releaseByTagQuery fetches a single release with filtered assets (includes SHA256 digest)
type releaseByTagQuery struct {
	Repository struct {
		Release struct {
			Name         string
			TagName      string
			PublishedAt  time.Time
			IsPrerelease bool
			TagCommit    struct {
				Oid string
			}
			ReleaseAssets struct {
				Nodes []struct {
					Name        string
					DownloadUrl string
					Digest      string // SHA256 digest as a string
				}
			} `graphql:"releaseAssets(first: 100, name: $assetName)"`
		} `graphql:"release(tagName: $tagName)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// releaseAllAssetsQuery fetches ALL assets from a release with SHA256 digests and pagination
type releaseAllAssetsQuery struct {
	Repository struct {
		Release struct {
			TagName       string
			PublishedAt   time.Time
			IsPrerelease  bool
			ReleaseAssets struct {
				Nodes []struct {
					Name        string
					DownloadUrl string
					Digest      string // SHA256 digest as a string
				}
				PageInfo struct {
					HasNextPage bool
					EndCursor   githubv4.String
				}
			} `graphql:"releaseAssets(first: $first, after: $after)"`
		} `graphql:"release(tagName: $tagName)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// Internal structs for release data

// ReleaseInfo represents a GitHub release with its assets
type ReleaseInfo struct {
	TagName         string
	PublishedAt     time.Time
	IsPrerelease    bool
	TargetCommitish string
	Assets          []AssetInfo
}

// AssetInfo represents a GitHub release asset with its digest
type AssetInfo struct {
	Name               string
	BrowserDownloadURL string
	ID                 int64
	SHA256             string
}

// Name returns the manager identifier
func (m *GitHubReleaseManager) Name() string {
	return "github_release"
}

// DiscoverVersions returns the most recent versions from GitHub using git HTTP protocol.
// Falls back to GraphQL if git HTTP fails.
func (m *GitHubReleaseManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	if pkg.Repo == "" {
		return nil, fmt.Errorf("repo is required for GitHub releases")
	}

	parts := strings.Split(pkg.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s (expected owner/repo)", pkg.Repo)
	}
	owner, repo := parts[0], parts[1]

	// Skip semver filtering if version_expr will transform the tags
	opts := DiscoverVersionsViaGitOptions{
		SkipSemverFilter: pkg.VersionExpr != "",
	}

	// Use git HTTP protocol with fallback to GraphQL
	versions, err := DiscoverVersionsViaGitWithFallback(ctx, owner, repo, limit, func() ([]types.Version, error) {
		return m.discoverVersionsViaGraphQL(ctx, owner, repo, pkg, limit)
	}, opts)
	if err != nil {
		return nil, err
	}

	// Apply version expression filtering if specified
	if pkg.VersionExpr != "" {
		filteredVersions, err := versionpkg.ApplyVersionExpr(versions, pkg.VersionExpr)
		if err != nil {
			return nil, fmt.Errorf("failed to apply version_expr for %s: %w", pkg.Name, err)
		}
		versions = filteredVersions
		// Re-sort after transformation since version strings may have changed
		versionpkg.SortVersions(versions)
	}

	// Filter out versions that are not valid semantic versions after transformation
	versions = versionpkg.FilterToValidSemver(versions)

	// Apply limit if specified (git HTTP returns all, so we need to limit)
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}

	return versions, nil
}

// discoverVersionsViaGraphQL fetches versions using GitHub GraphQL API (fallback method)
func (m *GitHubReleaseManager) discoverVersionsViaGraphQL(ctx context.Context, owner, repo string, pkg types.Package, limit int) ([]types.Version, error) {
	// Set appropriate page size based on limit
	first := limit
	if first <= 0 || first > 100 {
		first = 100
	}

	graphql := GetClient().GraphQL()
	var query releasesQuery
	variables := map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(repo),
		"first": githubv4.Int(first),
	}

	err := graphql.Query(ctx, &query, variables)
	if err != nil {
		err = m.enhanceRateLimitError(ctx, err)
		return nil, fmt.Errorf("failed to list releases for %s/%s: %w", owner, repo, err)
	}

	// Extract version information from GraphQL response
	var versions []types.Version
	for _, release := range query.Repository.Releases.Nodes {
		v := types.ParseVersion(versionpkg.Normalize(release.TagName), release.TagName)
		v.Published = release.PublishedAt
		v.SHA = release.TagCommit.Oid
		if release.IsPrerelease {
			v.Prerelease = true
		}
		versions = append(versions, v)
	}

	// Sort versions in descending order (newest first)
	versionpkg.SortVersions(versions)

	return versions, nil
}

// DiscoverVersionsViaREST fetches versions using GitHub REST API.
// This includes published dates but is subject to rate limits.
func (m *GitHubReleaseManager) DiscoverVersionsViaREST(ctx context.Context, pkg types.Package, limit int) ([]types.Version, error) {
	if pkg.Repo == "" {
		return nil, fmt.Errorf("repo is required for GitHub releases")
	}

	parts := strings.Split(pkg.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s (expected owner/repo)", pkg.Repo)
	}
	owner, repo := parts[0], parts[1]

	client := GetClient().Client()
	opts := &github.ListOptions{PerPage: limit}
	if limit <= 0 {
		opts.PerPage = 100
	}

	releases, _, err := client.Repositories.ListReleases(ctx, owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list releases for %s/%s: %w", owner, repo, err)
	}

	var versions []types.Version
	for _, rel := range releases {
		tagName := rel.GetTagName()
		v := types.ParseVersion(versionpkg.Normalize(tagName), tagName)
		v.Published = rel.GetPublishedAt().Time
		if rel.GetPrerelease() {
			v.Prerelease = true
		}
		if rel.GetTargetCommitish() != "" {
			v.SHA = rel.GetTargetCommitish()
		}
		versions = append(versions, v)
	}

	// Apply version expression filtering if specified
	if pkg.VersionExpr != "" {
		filteredVersions, err := versionpkg.ApplyVersionExpr(versions, pkg.VersionExpr)
		if err != nil {
			return nil, fmt.Errorf("failed to apply version_expr for %s: %w", pkg.Name, err)
		}
		versions = filteredVersions
	}

	versionpkg.SortVersions(versions)

	return versions, nil
}

// Resolve gets the download URL and metadata for a specific version and platform
func (m *GitHubReleaseManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	parts := strings.Split(pkg.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s", pkg.Repo)
	}
	owner, repo := parts[0], parts[1]

	// Debug: GitHub resolve: repo=%s/%s, version=%s, platform=%s

	// Find the release tag by version
	tagName, err := m.findReleaseByVersion(ctx, owner, repo, version, pkg.VersionExpr)
	if err != nil {
		// If it's a version not found error, enhance it with available versions
		if versionErr, ok := err.(*manager.ErrVersionNotFound); ok {
			return nil, m.enhanceErrorWithVersions(ctx, pkg, versionErr.Version, plat, err)
		}
		return nil, err
	}

	// Get the asset pattern for this platform or use fallback
	platformKey := plat.String()
	assetPattern := ""

	// Use common asset pattern resolution
	if pkg.AssetPatterns != nil {
		logger.V(4).Infof("Looking for asset pattern for platform: %s", platformKey)
		logger.V(4).Infof("Available asset patterns: %+v", pkg.AssetPatterns)
		var err error
		assetPattern, err = manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
		if err != nil {
			logger.V(4).Infof("Failed to resolve asset pattern: %v", err)
			// Continue with empty assetPattern to fall back to default
		} else {
			logger.V(4).Infof("Selected asset pattern: %s", assetPattern)
		}
	}

	// If no asset pattern found, fall back to url_template or default pattern
	if assetPattern == "" {
		if pkg.URLTemplate != "" {
			// Use url_template as fallback - we'll handle this later in the function
			assetPattern = "{{.name}}-{{.os}}-{{.arch}}"
		} else {
			// Use default pattern for GitHub releases
			assetPattern = "{{.name}}-{{.os}}-{{.arch}}"
		}
	}

	// Apply version_expr to the tag to get the version for templating
	versionForTemplate := version
	if pkg.VersionExpr != "" {
		testVer := types.Version{
			Tag:     tagName,
			Version: versionpkg.Normalize(tagName),
		}
		transformed, transformErr := versionpkg.ApplyVersionExpr([]types.Version{testVer}, pkg.VersionExpr)
		if transformErr == nil && len(transformed) > 0 {
			versionForTemplate = transformed[0].Version
			logger.V(4).Infof("Applied version_expr for templating: %s -> %s", tagName, versionForTemplate)
		}
	} else {
		versionForTemplate = versionpkg.Normalize(version)
	}

	// Template the asset pattern
	templateData := map[string]string{
		"name":    pkg.Name,
		"version": versionForTemplate,
		"tag":     tagName,
		"os":      plat.OS,
		"arch":    plat.Arch,
	}
	logger.V(5).Infof("Template data: %+v", templateData)

	templatedPattern, err := m.templateString(assetPattern, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to template asset pattern: %w", err)
	}

	// Debug: GitHub asset pattern templated: %s -> %s

	var downloadURL string
	var isArchive bool
	var githubAsset *types.GitHubAsset
	var assetSHA256 string // SHA256 digest from GraphQL asset query

	// Check if the templated pattern itself is a URL (URL override)
	if hasURLSchema(templatedPattern) {
		// The asset pattern contains a direct URL, use it
		downloadURL = templatedPattern
		isArchive = isArchiveFile(templatedPattern)

		// Debug: GitHub using direct URL from asset pattern: %s
	} else if pkg.URLTemplate != "" {
		// Debug: GitHub using URL template: %s

		// Normalize URL template to auto-append {{.asset}} if it ends with /
		urlTemplate := manager.NormalizeURLTemplate(pkg.URLTemplate)

		// Use the URL template instead of GitHub release assets
		downloadURL, err = m.templateString(urlTemplate, map[string]string{
			"name":    pkg.Name,
			"version": depstemplate.NormalizeVersion(version), // normalized without "v" prefix
			"tag":     tagName,
			"os":      plat.OS,
			"arch":    plat.Arch,
			"asset":   templatedPattern,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to template URL: %w", err)
		}
		isArchive = isArchiveFile(downloadURL)

		// Debug: GitHub templated URL: %s
	} else {

		// First try to fetch the asset by exact name using GraphQL
		asset, err := m.fetchReleaseAssetByName(ctx, owner, repo, tagName, templatedPattern)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch asset: %w", err)
		}

		if asset == nil {
			logger.V(3).Infof("No matching asset found for pattern: %s", templatedPattern)
			// Fetch all asset names and digests for filtering fallback
			allAssets, err := fetchAllReleaseAssetsWithDigests(ctx, owner, repo, tagName)
			if err != nil {
				// If we can't get asset list, return a basic error
				return nil, fmt.Errorf("asset not found: %s", templatedPattern)
			}

			// Convert to manager.AssetInfo format for filtering
			filterAssets := make([]manager.AssetInfo, len(allAssets))
			for i, a := range allAssets {
				filterAssets[i] = manager.AssetInfo{
					Name:        a.Name,
					DownloadURL: a.BrowserDownloadURL,
					SHA256:      a.SHA256,
				}
			}

			// Try iterative filtering as fallback
			logger.V(3).Infof("Attempting to filter %d assets by platform: %s", len(filterAssets), platformKey)
			filtered, filterErr := manager.FilterAssetsByPlatform(filterAssets, plat.OS, plat.Arch)
			if filterErr == nil && len(filtered) == 1 {
				// Found exactly one asset through filtering - use it
				downloadURL = filtered[0].DownloadURL
				isArchive = isArchiveFile(filtered[0].Name)
				assetSHA256 = filtered[0].SHA256
				githubAsset = &types.GitHubAsset{
					Repo:        pkg.Repo,
					Tag:         tagName,
					AssetName:   filtered[0].Name,
					DownloadURL: filtered[0].DownloadURL,
				}
				// Successfully found asset through filtering - continue with download
				goto assetFound
			} else if filterErr == nil && len(filtered) > 1 {
				logger.V(3).Infof("Filtering produced %d candidates, cannot determine which to use: %v", len(filtered), filtered)
			} else if filterErr != nil {
				logger.V(3).Infof("Filtering failed: %v", filterErr)
			}

			// Filtering didn't produce a single result - create enhanced error
			availableAssetNames := make([]string, len(allAssets))
			for i, a := range allAssets {
				availableAssetNames[i] = a.Name
			}

			assetErr := &manager.ErrAssetNotFound{
				Package:         pkg.Name,
				AssetPattern:    templatedPattern,
				Platform:        platformKey,
				AvailableAssets: availableAssetNames,
			}

			// Enhance the error with available assets and suggestions
			return nil, manager.EnhanceAssetNotFoundError(pkg.Name, templatedPattern, platformKey, availableAssetNames, assetErr)
		}

	assetFound:
		// Set asset info if we found it via exact match (not filtering)
		if asset != nil {
			// Debug: GitHub found matching asset: %s
			downloadURL = asset.BrowserDownloadURL
			isArchive = isArchiveFile(asset.Name)
			assetSHA256 = asset.SHA256 // Store the SHA256 digest from GraphQL
			githubAsset = &types.GitHubAsset{
				Repo:        pkg.Repo,
				Tag:         tagName,
				AssetName:   asset.Name,
				AssetID:     asset.ID,
				DownloadURL: asset.BrowserDownloadURL,
			}
		}
	}

	resolution := &types.Resolution{
		Package:     pkg,
		Version:     version,
		Platform:    plat,
		DownloadURL: downloadURL,
		IsArchive:   isArchive,
		GitHubAsset: githubAsset,
	}

	// Set binary path for archives
	if resolution.IsArchive {
		assetName := templatedPattern
		if githubAsset != nil {
			assetName = githubAsset.AssetName
		}
		resolution.BinaryPath = m.guessBinaryPath(pkg, assetName, plat)
	}

	// Set checksum from asset digest - this should always be available from GraphQL
	if assetSHA256 != "" {
		logger.V(3).Infof("Using SHA256 digest from GitHub asset: %s", assetSHA256)
		// GitHub returns digest with sha256: prefix, but we've already stripped it, so add it back
		resolution.Checksum = "sha256:" + assetSHA256
	} else if githubAsset != nil {
		// Older releases may not have digest field populated in GraphQL - log warning
		logger.Infof("\033[31mNo digest available from GraphQL for asset %s (repo: %s, tag: %s) - will try checksum files\033[0m",
			githubAsset.AssetName, githubAsset.Repo, githubAsset.Tag)
	}

	// Find the matching asset using GraphQL with name filter
	logger.Debugf("Resolved %s", resolution.Pretty().ANSI())

	return resolution, nil
}

// Install downloads and installs the binary
func (m *GitHubReleaseManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	// For now, return not implemented - the actual installation
	// is handled by the existing deps.Install function
	// TODO: Implement proper installation using download package
	return fmt.Errorf("install method not yet implemented - use existing Install")
}

// GetChecksums returns nil since GitHub release assets include SHA256 digest
// The digest is automatically included in the Resolution via the asset's digest field
func (m *GitHubReleaseManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	// GitHub release assets include SHA256 digest in the GraphQL API response.
	// The Resolve method automatically captures this digest and sets it in Resolution.Checksum
	// No need to download separate checksum files
	return nil, nil
}

// Verify checks if an installed binary matches expectations
func (m *GitHubReleaseManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	// TODO: Implement verification logic
	return nil, fmt.Errorf("verify not implemented yet")
}

// WhoAmI returns authentication status and user information for GitHub
func (m *GitHubReleaseManager) WhoAmI(ctx context.Context) *types.AuthStatus {
	client := GetClient().Client()
	status := &types.AuthStatus{
		Service:     "GitHub",
		TokenSource: GetClient().TokenSource(),
	}

	// Get authenticated user information
	user, response, err := client.Users.Get(ctx, "")
	if err != nil {
		status.Authenticated = false
		status.Error = fmt.Sprintf("Failed to get user info: %v", err)
		status.HasPermissions = false

		// Still try to get rate limit info even if auth failed
		if response != nil {
			status.RateLimit = extractRateLimit(response)
		}
		return status
	}

	status.Authenticated = true
	status.HasPermissions = true // GitHub releases don't require special scopes

	// Fill user information
	if user != nil {
		userInfo := &types.UserInfo{
			Username: user.GetLogin(),
			Name:     user.GetName(),
			Email:    user.GetEmail(),
			Company:  user.GetCompany(),
		}

		if createdAt := user.GetCreatedAt(); !createdAt.IsZero() {
			userInfo.CreatedAt = &createdAt.Time
		}

		status.User = userInfo
	}

	// Extract rate limit information
	if response != nil {
		status.RateLimit = extractRateLimit(response)
	}

	return status
}

// extractRateLimit extracts rate limit information from GitHub API response
func extractRateLimit(response *github.Response) *types.RateLimit {
	if response == nil || response.Rate.Limit == 0 {
		return nil
	}

	resetTime := response.Rate.Reset.Time
	return &types.RateLimit{
		Remaining: response.Rate.Remaining,
		Total:     response.Rate.Limit,
		ResetTime: &resetTime,
	}
}

// isRateLimitError checks if an error is a GitHub rate limit error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	// Check for primary rate limit error
	var rateLimitErr *github.RateLimitError
	if errors.As(err, &rateLimitErr) {
		return true
	}

	// Check for secondary/abuse rate limit error
	var abuseErr *github.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		return true
	}

	// Check for rate limit in error message (for GraphQL and other wrapped errors)
	errMsg := err.Error()
	if strings.Contains(errMsg, "API rate limit exceeded") ||
		strings.Contains(errMsg, "403 Forbidden") ||
		strings.Contains(errMsg, "rate limit") {
		return true
	}

	return false
}

// enhanceRateLimitError wraps a rate limit error with current rate limit status
func (m *GitHubReleaseManager) enhanceRateLimitError(ctx context.Context, originalErr error) error {
	if originalErr == nil || !isRateLimitError(originalErr) {
		return originalErr
	}

	// Get current rate limit status via WhoAmI
	status := m.WhoAmI(ctx)

	// Build compact error message
	var msg strings.Builder
	msg.WriteString(originalErr.Error())

	// Get rate limit details
	var remaining, total int
	var resetTime *time.Time

	// Try to get from structured error first
	var rateLimitErr *github.RateLimitError
	if errors.As(originalErr, &rateLimitErr) {
		remaining = rateLimitErr.Rate.Remaining
		total = rateLimitErr.Rate.Limit
		if !rateLimitErr.Rate.Reset.IsZero() {
			t := rateLimitErr.Rate.Reset.Time
			resetTime = &t
		}
	} else if status.RateLimit != nil {
		// Fall back to WhoAmI rate limit
		remaining = status.RateLimit.Remaining
		total = status.RateLimit.Total
		resetTime = status.RateLimit.ResetTime
	}

	// Build compact single-line message
	msg.WriteString(". GitHub API rate limit")
	if total > 0 {
		msg.WriteString(fmt.Sprintf(": %d/%d remaining", remaining, total))
	} else {
		msg.WriteString(" exceeded")
	}

	if resetTime != nil {
		timeUntilReset := time.Until(*resetTime)
		msg.WriteString(fmt.Sprintf(", resets in %s", formatDuration(timeUntilReset)))
	}

	if status.TokenSource != "" {
		msg.WriteString(fmt.Sprintf(" (using %s)", status.TokenSource))
	} else {
		msg.WriteString(". Set GITHUB_TOKEN for 5000/hour limit")
	}

	return fmt.Errorf("%s", msg.String())
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "expired"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}

// Helper methods

func (m *GitHubReleaseManager) findReleaseByVersion(ctx context.Context, owner, repo, targetVersion, versionExpr string) (string, error) {
	logger.V(3).Infof("GitHub fetching releases for %s/%s, looking for version: %s", owner, repo, targetVersion)

	graphql := GetClient().GraphQL()
	var query releasesQuery
	variables := map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(repo),
		"first": githubv4.Int(100),
	}

	err := graphql.Query(ctx, &query, variables)
	if err != nil {
		err = m.enhanceRateLimitError(ctx, err)
		return "", fmt.Errorf("failed to list releases: %w", err)
	}

	releases := query.Repository.Releases.Nodes

	logger.V(4).Infof("GitHub found %d releases, checking for version %s", len(releases), targetVersion)
	if logger.IsLevelEnabled(4) {
		tagNames := make([]string, 0, min(6, len(releases)))
		for i, rel := range releases {
			tagNames = append(tagNames, rel.TagName)
			if i >= 5 {
				break
			}
		}
		logger.V(4).Infof("First releases: %v (and %d more)", tagNames, max(0, len(releases)-6))
	}

	// Try exact tag match first
	logger.V(4).Infof("Trying exact tag match for: %s or v%s", targetVersion, targetVersion)
	for _, rel := range releases {
		if rel.TagName == targetVersion || rel.TagName == "v"+targetVersion {
			logger.V(3).Infof("Found exact tag match: %s", rel.TagName)
			return rel.TagName, nil
		}
	}

	// Try version normalization match
	normalizedTarget := versionpkg.Normalize(targetVersion)
	for _, rel := range releases {
		if versionpkg.Normalize(rel.TagName) == normalizedTarget {
			return rel.TagName, nil
		}
	}

	// If version_expr is provided, try applying it to each tag and see if it matches targetVersion
	if versionExpr != "" {
		logger.V(4).Infof("Trying version_expr match with expr: %s", versionExpr)
		for _, rel := range releases {
			// Apply version_expr to this tag
			testVersion := types.Version{
				Tag:     rel.TagName,
				Version: versionpkg.Normalize(rel.TagName),
			}
			transformed, err := versionpkg.ApplyVersionExpr([]types.Version{testVersion}, versionExpr)
			if err != nil {
				logger.V(1).Infof("Failed to apply version_expr to tag %s: %v", rel.TagName, err)
				continue
			}

			// Check if the transformed version matches our target
			if len(transformed) > 0 && transformed[0].Version == targetVersion {
				logger.V(3).Infof("Found version_expr match: tag %s transformed to %s", rel.TagName, transformed[0].Version)
				return rel.TagName, nil
			}
		}
	}

	return "", &manager.ErrVersionNotFound{
		Package: repo,
		Version: targetVersion,
	}
}

// fetchReleaseAssetByName queries GraphQL for a specific asset by name with its digest
func (m *GitHubReleaseManager) fetchReleaseAssetByName(ctx context.Context, owner, repo, tagName, assetName string) (*AssetInfo, error) {
	graphql := GetClient().GraphQL()
	var query releaseByTagQuery
	variables := map[string]interface{}{
		"owner":     githubv4.String(owner),
		"name":      githubv4.String(repo),
		"tagName":   githubv4.String(tagName),
		"assetName": githubv4.String(assetName),
	}

	err := graphql.Query(ctx, &query, variables)
	if err != nil {
		err = m.enhanceRateLimitError(ctx, err)
		return nil, fmt.Errorf("failed to query release assets: %w", err)
	}

	// Check if asset was found
	if len(query.Repository.Release.ReleaseAssets.Nodes) == 0 {
		return nil, nil // Asset not found
	}

	// Return the first matching asset (should be exactly one due to name filter)
	node := query.Repository.Release.ReleaseAssets.Nodes[0]
	return &AssetInfo{
		Name:               node.Name,
		BrowserDownloadURL: node.DownloadUrl,
		ID:                 0, // AssetID not available in GraphQL schema and not used
		SHA256:             stripChecksumPrefix(node.Digest),
	}, nil
}

// fetchAllReleaseAssetsWithDigests queries GraphQL for ALL assets with SHA256 digests and pagination
// This is a package-level function that both GitHubReleaseManager and GitHubBuildManager can use
func fetchAllReleaseAssetsWithDigests(ctx context.Context, owner, repo, tagName string) ([]AssetInfo, error) {
	graphql := GetClient().GraphQL()
	var allAssets []AssetInfo
	var after *githubv4.String

	// Handle pagination - loop until we've fetched all assets
	for {
		var query releaseAllAssetsQuery
		variables := map[string]interface{}{
			"owner":   githubv4.String(owner),
			"name":    githubv4.String(repo),
			"tagName": githubv4.String(tagName),
			"first":   githubv4.Int(100),
			"after":   after,
		}

		err := graphql.Query(ctx, &query, variables)
		if err != nil {
			// Create a temporary GitHubReleaseManager to use enhanceRateLimitError
			tempMgr := &GitHubReleaseManager{}
			err = tempMgr.enhanceRateLimitError(ctx, err)
			return nil, fmt.Errorf("failed to query release assets: %w", err)
		}

		// Extract assets from this page
		for _, node := range query.Repository.Release.ReleaseAssets.Nodes {
			allAssets = append(allAssets, AssetInfo{
				Name:               node.Name,
				BrowserDownloadURL: node.DownloadUrl,
				ID:                 0, // Not available in GraphQL schema
				SHA256:             stripChecksumPrefix(node.Digest),
			})
		}

		// Check if there are more pages
		if !query.Repository.Release.ReleaseAssets.PageInfo.HasNextPage {
			break
		}

		// Set cursor for next page
		after = &query.Repository.Release.ReleaseAssets.PageInfo.EndCursor
	}

	return allAssets, nil
}

func (m *GitHubReleaseManager) templateString(pattern string, data map[string]string) (string, error) {
	return depstemplate.TemplateString(pattern, data)
}

// enhanceErrorWithVersions enhances version not found errors with available version suggestions
func (m *GitHubReleaseManager) enhanceErrorWithVersions(ctx context.Context, pkg types.Package, requestedVersion string, plat platform.Platform, originalErr error) error {
	// Try to get available versions using a default platform for error enhancement
	versions, err := m.DiscoverVersions(ctx, pkg, plat, 20)
	if err != nil {
		// If we can't get versions, return the original error
		return originalErr
	}

	return manager.EnhanceErrorWithVersions(pkg.Name, requestedVersion, versions, originalErr)
}

func (m *GitHubReleaseManager) guessBinaryPath(pkg types.Package, assetName string, plat platform.Platform) string {
	// First check if BinaryPath is specified (supports CEL expressions)
	if pkg.BinaryPath != "" {
		// Evaluate CEL expression or template
		data := map[string]interface{}{
			"os":   plat.OS,
			"arch": plat.Arch,
			"name": pkg.Name,
		}

		result, err := depstemplate.EvaluateCELOrTemplate(pkg.BinaryPath, data)
		if err == nil && result != "" {
			return result
		}
		// If CEL evaluation fails, fall back to treating it as a literal string
		return pkg.BinaryPath
	}

	// Fall back to BinaryName if specified
	if pkg.BinaryName != "" {
		return pkg.BinaryName
	}

	// Common patterns for binary paths in archives
	baseName := pkg.Name
	if plat.IsWindows() {
		baseName += ".exe"
	}

	// Try common patterns
	patterns := []string{
		baseName, // just the binary name
		fmt.Sprintf("%s/%s", plat.String(), baseName),         // platform-specific subdirectory
		fmt.Sprintf("%s-%s/%s", plat.OS, plat.Arch, baseName), // os-arch subdirectory
	}

	// Return the first pattern (most common case)
	return patterns[0]
}

// stripChecksumPrefix removes the checksum type prefix (e.g., "sha256:") from a digest string
// Returns the raw hex string, or empty string if input is empty
func stripChecksumPrefix(digest string) string {
	if digest == "" {
		return ""
	}
	value, _ := checksum.ParseChecksum(digest)
	return value
}

// isArchiveFile returns true if the filename appears to be an archive
func isArchiveFile(filename string) bool {
	archiveExtensions := []string{
		".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz",
		".zip", ".7z", ".rar",
	}

	filename = strings.ToLower(filename)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(filename, ext) {
			return true
		}
	}
	return false
}

// hasURLSchema returns true if the string appears to be a URL with schema
func hasURLSchema(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// These functions are no longer needed - moved to manager.ResolveAssetPattern
