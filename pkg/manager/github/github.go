package github

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
)

// GitHubReleaseManager implements the PackageManager interface for GitHub releases
type GitHubReleaseManager struct{}

// NewGitHubReleaseManager creates a new GitHub release manager.
func NewGitHubReleaseManager() *GitHubReleaseManager {
	return &GitHubReleaseManager{}
}

// REST API response types

// restRelease represents a GitHub release from REST API
type restRelease struct {
	ID          int64            `json:"id"`
	TagName     string           `json:"tag_name"`
	Name        string           `json:"name"`
	Prerelease  bool             `json:"prerelease"`
	Draft       bool             `json:"draft"`
	PublishedAt time.Time        `json:"published_at"`
	Assets      []restAsset      `json:"assets"`
	Author      *restUser        `json:"author,omitempty"`
	TargetCommitish string       `json:"target_commitish,omitempty"`
}

// restAsset represents a release asset from REST API (includes digest field)
type restAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"` // "sha256:..." - available in REST API
	Size               int    `json:"size"`
	ContentType        string `json:"content_type"`
}

// restUser represents a GitHub user from REST API
type restUser struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
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
// Falls back to REST API if git HTTP fails.
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

	// Use git HTTP protocol with fallback to REST API
	versions, err := DiscoverVersionsViaGitWithFallback(ctx, owner, repo, limit, func() ([]types.Version, error) {
		return m.discoverVersionsViaREST(ctx, owner, repo, limit)
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

// discoverVersionsViaREST fetches versions using GitHub REST API (fallback method)
func (m *GitHubReleaseManager) discoverVersionsViaREST(ctx context.Context, owner, repo string, limit int) ([]types.Version, error) {
	perPage := limit
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}

	endpoint := fmt.Sprintf("/repos/%s/%s/releases?per_page=%d", owner, repo, perPage)
	var releases []restRelease
	if err := GetClient().RESTRequest(ctx, "GET", endpoint, &releases); err != nil {
		return nil, fmt.Errorf("failed to list releases for %s/%s: %w", owner, repo, err)
	}

	var versions []types.Version
	for _, release := range releases {
		if release.Draft {
			continue // Skip draft releases
		}
		v := types.ParseVersion(versionpkg.Normalize(release.TagName), release.TagName)
		v.Published = release.PublishedAt
		v.Prerelease = release.Prerelease
		if release.TargetCommitish != "" {
			v.SHA = release.TargetCommitish
		}
		versions = append(versions, v)
	}

	// Sort versions in descending order (newest first)
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

	// Fast path: use REST API for "latest" when no version_expr is configured
	var tagName string
	if version == "latest" && pkg.VersionExpr == "" && pkg.URLTemplate == "" {
		logger.V(3).Infof("Using REST API fast path for latest release of %s/%s", owner, repo)
		release, err := m.fetchReleaseViaREST(ctx, owner, repo, "latest")
		if err == nil {
			resolution, buildErr := m.buildResolutionFromRelease(pkg, release, plat)
			if buildErr == nil {
				return resolution, nil
			}
			// REST fetched the release but couldn't build resolution (asset not found)
			// Use the tag from REST for the fallback path
			logger.V(3).Infof("REST fast path failed to build resolution: %v, using tag %s for fallback", buildErr, release.TagName)
			tagName = release.TagName
		} else {
			// Check for rate limit error - try fallback
			if isRateLimitError(err) {
				return m.handleRateLimitFallback(ctx, pkg, version, plat, err)
			}
			logger.V(3).Infof("REST fast path failed: %v, falling back to normal path", err)
		}
	}

	// Find the release tag by version (if not already determined by REST fast path)
	if tagName == "" {
		var err error
		tagName, err = m.findReleaseByVersion(ctx, owner, repo, version, pkg.VersionExpr)
		if err != nil {
			// Check for rate limit error - try fallback
			if isRateLimitError(err) {
				return m.handleRateLimitFallback(ctx, pkg, version, plat, err)
			}
			// If it's a version not found error, enhance it with available versions
			if versionErr, ok := err.(*manager.ErrVersionNotFound); ok {
				return nil, m.enhanceErrorWithVersions(ctx, pkg, versionErr.Version, plat, err)
			}
			return nil, err
		}
	}

	// Try REST API for explicit versions (to get digest field in single call)
	// Skip if url_template is configured (custom download URLs)
	if pkg.URLTemplate == "" {
		logger.V(3).Infof("Using REST API for release %s/%s tag %s", owner, repo, tagName)
		release, restErr := m.fetchReleaseViaREST(ctx, owner, repo, "tags/"+tagName)
		if restErr == nil {
			resolution, buildErr := m.buildResolutionFromRelease(pkg, release, plat)
			if buildErr == nil {
				// Preserve the original requested version (not the tag)
				resolution.Version = version
				return resolution, nil
			}
			logger.V(3).Infof("REST API failed to build resolution: %v, falling back to go-github", buildErr)
		} else {
			logger.V(3).Infof("REST API failed: %v, falling back to go-github", restErr)
		}
	}

	// Fallback: use go-github library for URL template cases or when REST fails
	return m.resolveViaGoGitHub(ctx, pkg, version, tagName, plat)
}

// fetchReleaseViaREST fetches a release using REST API (includes digest field).
// endpoint is either "latest" or "tags/{tagName}"
func (m *GitHubReleaseManager) fetchReleaseViaREST(ctx context.Context, owner, repo, endpoint string) (*restRelease, error) {
	url := fmt.Sprintf("/repos/%s/%s/releases/%s", owner, repo, endpoint)
	var release restRelease
	if err := GetClient().RESTRequest(ctx, "GET", url, &release); err != nil {
		return nil, err
	}
	return &release, nil
}

// buildResolutionFromRelease builds a Resolution from REST API release response
func (m *GitHubReleaseManager) buildResolutionFromRelease(pkg types.Package, release *restRelease, plat platform.Platform) (*types.Resolution, error) {
	tagName := release.TagName
	version := versionpkg.Normalize(tagName)

	// Get the asset pattern for this platform
	assetPattern, _ := manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
	if assetPattern == "" {
		assetPattern = "{{.name}}-{{.os}}-{{.arch}}"
	}

	// Template the asset pattern
	templatedPattern, err := m.templateString(assetPattern, map[string]string{
		"name": pkg.Name, "version": version, "tag": tagName,
		"os": plat.OS, "arch": plat.Arch,
	})
	if err != nil {
		return nil, err
	}

	// Find matching asset - support both exact match and glob patterns
	var matched *restAsset
	for i, asset := range release.Assets {
		if asset.Name == templatedPattern {
			matched = &release.Assets[i]
			break
		}
		// Try glob matching if pattern contains wildcards
		if strings.Contains(templatedPattern, "*") || strings.Contains(templatedPattern, "?") {
			if ok, _ := filepath.Match(templatedPattern, asset.Name); ok {
				matched = &release.Assets[i]
				break
			}
		}
	}

	if matched == nil {
		// Fallback: filter by platform
		assets := make([]manager.AssetInfo, len(release.Assets))
		for i, a := range release.Assets {
			assets[i] = manager.AssetInfo{
				Name:        a.Name,
				DownloadURL: a.BrowserDownloadURL,
				SHA256:      stripChecksumPrefix(a.Digest),
			}
		}
		filtered, filterErr := manager.FilterAssetsByPlatform(assets, plat.OS, plat.Arch)
		if filterErr == nil && len(filtered) == 1 {
			for i, a := range release.Assets {
				if a.Name == filtered[0].Name {
					matched = &release.Assets[i]
					break
				}
			}
		}
	}

	if matched == nil {
		availableAssetNames := make([]string, len(release.Assets))
		for i, a := range release.Assets {
			availableAssetNames[i] = a.Name
		}
		return nil, manager.EnhanceAssetNotFoundError(pkg.Name, templatedPattern, plat.String(), availableAssetNames,
			&manager.ErrAssetNotFound{
				Package:         pkg.Name,
				AssetPattern:    templatedPattern,
				Platform:        plat.String(),
				AvailableAssets: availableAssetNames,
			})
	}

	resolution := &types.Resolution{
		Package:     pkg,
		Version:     version,
		Platform:    plat,
		DownloadURL: matched.BrowserDownloadURL,
		IsArchive:   isArchiveFile(matched.Name),
		GitHubAsset: &types.GitHubAsset{
			Repo:        pkg.Repo,
			Tag:         tagName,
			AssetName:   matched.Name,
			AssetID:     matched.ID,
			DownloadURL: matched.BrowserDownloadURL,
		},
	}

	// Set checksum from digest if available
	if matched.Digest != "" {
		resolution.Checksum = matched.Digest // Already in "sha256:..." format from REST API
	}

	if resolution.IsArchive {
		resolution.BinaryPath = m.guessBinaryPath(pkg, matched.Name, plat)
	}

	logger.Debugf("Resolved %s", resolution.Pretty().ANSI())
	return resolution, nil
}

// resolveViaGoGitHub uses the go-github library for URL template cases
func (m *GitHubReleaseManager) resolveViaGoGitHub(ctx context.Context, pkg types.Package, version, tagName string, plat platform.Platform) (*types.Resolution, error) {
	parts := strings.Split(pkg.Repo, "/")
	owner, repo := parts[0], parts[1]

	// Get the asset pattern for this platform or use fallback
	platformKey := plat.String()
	assetPattern := ""

	if pkg.AssetPatterns != nil {
		logger.V(4).Infof("Looking for asset pattern for platform: %s", platformKey)
		var err error
		assetPattern, err = manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
		if err != nil {
			logger.V(4).Infof("Failed to resolve asset pattern: %v", err)
		} else {
			logger.V(4).Infof("Selected asset pattern: %s", assetPattern)
		}
	}

	if assetPattern == "" {
		assetPattern = "{{.name}}-{{.os}}-{{.arch}}"
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

	templatedPattern, err := m.templateString(assetPattern, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to template asset pattern: %w", err)
	}

	var downloadURL string
	var isArchive bool
	var githubAsset *types.GitHubAsset
	var assetSHA256 string

	// Check if the templated pattern itself is a URL (URL override)
	if hasURLSchema(templatedPattern) {
		downloadURL = templatedPattern
		isArchive = isArchiveFile(templatedPattern)
	} else if pkg.URLTemplate != "" {
		// Normalize URL template to auto-append {{.asset}} if it ends with /
		urlTemplate := manager.NormalizeURLTemplate(pkg.URLTemplate)

		downloadURL, err = m.templateString(urlTemplate, map[string]string{
			"name":    pkg.Name,
			"version": depstemplate.NormalizeVersion(version),
			"tag":     tagName,
			"os":      plat.OS,
			"arch":    plat.Arch,
			"asset":   templatedPattern,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to template URL: %w", err)
		}
		isArchive = isArchiveFile(downloadURL)
	} else {
		// Try to fetch release assets via REST API
		release, err := m.fetchReleaseViaREST(ctx, owner, repo, "tags/"+tagName)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch release: %w", err)
		}

		// Find matching asset - support both exact match and glob patterns
		var matchedAsset *restAsset
		for i, asset := range release.Assets {
			if asset.Name == templatedPattern {
				matchedAsset = &release.Assets[i]
				break
			}
			// Try glob matching if pattern contains wildcards
			if strings.Contains(templatedPattern, "*") || strings.Contains(templatedPattern, "?") {
				if ok, _ := filepath.Match(templatedPattern, asset.Name); ok {
					matchedAsset = &release.Assets[i]
					break
				}
			}
		}

		if matchedAsset == nil {
			// Fallback: filter by platform
			assets := make([]manager.AssetInfo, len(release.Assets))
			for i, a := range release.Assets {
				assets[i] = manager.AssetInfo{
					Name:        a.Name,
					DownloadURL: a.BrowserDownloadURL,
					SHA256:      stripChecksumPrefix(a.Digest),
				}
			}

			filtered, filterErr := manager.FilterAssetsByPlatform(assets, plat.OS, plat.Arch)
			if filterErr == nil && len(filtered) == 1 {
				downloadURL = filtered[0].DownloadURL
				isArchive = isArchiveFile(filtered[0].Name)
				assetSHA256 = filtered[0].SHA256
				githubAsset = &types.GitHubAsset{
					Repo:        pkg.Repo,
					Tag:         tagName,
					AssetName:   filtered[0].Name,
					DownloadURL: filtered[0].DownloadURL,
				}
				goto assetFound
			}

			// Create enhanced error
			availableAssetNames := make([]string, len(release.Assets))
			for i, a := range release.Assets {
				availableAssetNames[i] = a.Name
			}

			return nil, manager.EnhanceAssetNotFoundError(pkg.Name, templatedPattern, platformKey, availableAssetNames,
				&manager.ErrAssetNotFound{
					Package:         pkg.Name,
					AssetPattern:    templatedPattern,
					Platform:        platformKey,
					AvailableAssets: availableAssetNames,
				})
		}

		downloadURL = matchedAsset.BrowserDownloadURL
		isArchive = isArchiveFile(matchedAsset.Name)
		assetSHA256 = stripChecksumPrefix(matchedAsset.Digest)
		githubAsset = &types.GitHubAsset{
			Repo:        pkg.Repo,
			Tag:         tagName,
			AssetName:   matchedAsset.Name,
			AssetID:     matchedAsset.ID,
			DownloadURL: matchedAsset.BrowserDownloadURL,
		}
	}

assetFound:
	resolution := &types.Resolution{
		Package:     pkg,
		Version:     version,
		Platform:    plat,
		DownloadURL: downloadURL,
		IsArchive:   isArchive,
		GitHubAsset: githubAsset,
	}

	if resolution.IsArchive {
		assetName := templatedPattern
		if githubAsset != nil {
			assetName = githubAsset.AssetName
		}
		resolution.BinaryPath = m.guessBinaryPath(pkg, assetName, plat)
	}

	if assetSHA256 != "" {
		logger.V(3).Infof("Using SHA256 digest from GitHub asset: %s", assetSHA256)
		resolution.Checksum = "sha256:" + assetSHA256
	}

	logger.Debugf("Resolved %s", resolution.Pretty().ANSI())
	return resolution, nil
}

// findReleaseByVersion finds a release tag by version using REST API
func (m *GitHubReleaseManager) findReleaseByVersion(ctx context.Context, owner, repo, targetVersion, versionExpr string) (string, error) {
	logger.V(3).Infof("GitHub fetching releases for %s/%s, looking for version: %s", owner, repo, targetVersion)

	// Fetch releases via REST API
	endpoint := fmt.Sprintf("/repos/%s/%s/releases?per_page=100", owner, repo)
	var releases []restRelease
	if err := GetClient().RESTRequest(ctx, "GET", endpoint, &releases); err != nil {
		return "", fmt.Errorf("failed to list releases: %w", err)
	}

	logger.V(4).Infof("GitHub found %d releases, checking for version %s", len(releases), targetVersion)

	// Handle "latest" - return the first non-prerelease, or first release if all are prereleases
	if targetVersion == "latest" {
		for _, rel := range releases {
			if !rel.Prerelease && !rel.Draft {
				logger.V(3).Infof("Found latest stable release: %s", rel.TagName)
				return rel.TagName, nil
			}
		}
		// No stable releases found, return first non-draft release
		for _, rel := range releases {
			if !rel.Draft {
				logger.V(3).Infof("No stable releases, using first release: %s", rel.TagName)
				return rel.TagName, nil
			}
		}
		return "", fmt.Errorf("no releases found for %s/%s", owner, repo)
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
			testVersion := types.Version{
				Tag:     rel.TagName,
				Version: versionpkg.Normalize(rel.TagName),
			}
			transformed, err := versionpkg.ApplyVersionExpr([]types.Version{testVersion}, versionExpr)
			if err != nil {
				logger.V(1).Infof("Failed to apply version_expr to tag %s: %v", rel.TagName, err)
				continue
			}

			if len(transformed) > 0 && (transformed[0].Version == targetVersion ||
				transformed[0].Version == normalizedTarget ||
				transformed[0].Tag == targetVersion) {
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

// fetchAllReleaseAssets fetches all assets from a release using REST API
func fetchAllReleaseAssets(ctx context.Context, owner, repo, tagName string) ([]AssetInfo, error) {
	endpoint := fmt.Sprintf("/repos/%s/%s/releases/tags/%s", owner, repo, tagName)
	var release restRelease
	if err := GetClient().RESTRequest(ctx, "GET", endpoint, &release); err != nil {
		return nil, fmt.Errorf("failed to fetch release: %w", err)
	}

	assets := make([]AssetInfo, len(release.Assets))
	for i, a := range release.Assets {
		assets[i] = AssetInfo{
			Name:               a.Name,
			BrowserDownloadURL: a.BrowserDownloadURL,
			ID:                 a.ID,
			SHA256:             stripChecksumPrefix(a.Digest),
		}
	}
	return assets, nil
}

// Install downloads and installs the binary
func (m *GitHubReleaseManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	return fmt.Errorf("install method not yet implemented - use existing Install")
}

// GetChecksums returns nil since GitHub release assets include SHA256 digest in REST API
func (m *GitHubReleaseManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	return nil, nil
}

// Verify checks if an installed binary matches expectations
func (m *GitHubReleaseManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	return nil, fmt.Errorf("verify not implemented yet")
}

// WhoAmI returns authentication status and user information for GitHub
func (m *GitHubReleaseManager) WhoAmI(ctx context.Context) *types.AuthStatus {
	client := GetClient().Client()
	status := &types.AuthStatus{
		Service:     "GitHub",
		TokenSource: GetClient().TokenSource(),
	}

	user, response, err := client.Users.Get(ctx, "")
	if err != nil {
		status.Authenticated = false
		status.Error = fmt.Sprintf("Failed to get user info: %v", err)
		status.HasPermissions = false

		if response != nil {
			status.RateLimit = extractRateLimit(response)
		}
		return status
	}

	status.Authenticated = true
	status.HasPermissions = true

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

	var rateLimitErr *github.RateLimitError
	if errors.As(err, &rateLimitErr) {
		return true
	}

	var abuseErr *github.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		return true
	}

	errMsg := err.Error()
	return strings.Contains(errMsg, "API rate limit exceeded") ||
		strings.Contains(errMsg, "403 Forbidden") ||
		strings.Contains(errMsg, "rate limit")
}

// handleRateLimitFallback handles rate limit errors by using fallback version
// If strict checksum mode is enabled, it returns the error. Otherwise, it builds
// a resolution without checksum using the fallback version and url_template or asset_patterns.
func (m *GitHubReleaseManager) handleRateLimitFallback(ctx context.Context, pkg types.Package, version string, plat platform.Platform, originalErr error) (*types.Resolution, error) {
	// Check if strict checksum mode is enabled
	if manager.GetStrictChecksum(ctx) {
		return nil, fmt.Errorf("rate limited and --strict-checksum requires checksum verification: %w", originalErr)
	}

	// Determine fallback version
	fallbackVersion := pkg.FallbackVersion
	if fallbackVersion == "" {
		fallbackVersion = "latest"
	}

	// Check if we have url_template or non-wildcard asset_patterns to build a fallback URL
	hasURLTemplate := pkg.URLTemplate != ""
	hasAssetPattern := m.hasNonWildcardAssetPattern(pkg, plat)

	if !hasURLTemplate && !hasAssetPattern {
		return nil, fmt.Errorf("rate limited and no url_template or asset_patterns configured for fallback: %w", originalErr)
	}

	logger.Warnf("GitHub rate limited, using fallback version '%s' without checksum verification", fallbackVersion)

	// Use the requested version for templating if it looks like a specific version
	versionForTemplate := version
	if version == "latest" || version == "" {
		versionForTemplate = fallbackVersion
	}

	// Build resolution without checksum
	return m.buildFallbackResolution(pkg, versionForTemplate, plat)
}

// hasNonWildcardAssetPattern checks if the package has an asset pattern for the platform
// that doesn't contain wildcards (can be used to construct a deterministic download URL)
func (m *GitHubReleaseManager) hasNonWildcardAssetPattern(pkg types.Package, plat platform.Platform) bool {
	if pkg.AssetPatterns == nil {
		return false
	}
	pattern, err := manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
	if err != nil || pattern == "" {
		return false
	}
	// Check if pattern contains wildcards
	return !strings.Contains(pattern, "*") && !strings.Contains(pattern, "?")
}

// buildFallbackResolution builds a resolution using url_template or asset_patterns without checksum
func (m *GitHubReleaseManager) buildFallbackResolution(pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	// Get the asset pattern for this platform
	assetPattern, _ := manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
	if assetPattern == "" {
		assetPattern = "{{.name}}-{{.os}}-{{.arch}}"
	}

	// Determine tag name (typically "v" + version for GitHub releases)
	tagName := version
	if !strings.HasPrefix(version, "v") {
		tagName = "v" + version
	}

	normalizedVersion := versionpkg.Normalize(version)

	// Template the asset pattern
	templatedPattern, err := m.templateString(assetPattern, map[string]string{
		"name": pkg.Name, "version": normalizedVersion, "tag": tagName,
		"os": plat.OS, "arch": plat.Arch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to template asset pattern: %w", err)
	}

	var downloadURL string
	if pkg.URLTemplate != "" {
		// Use url_template if provided
		urlTemplate := manager.NormalizeURLTemplate(pkg.URLTemplate)
		downloadURL, err = m.templateString(urlTemplate, map[string]string{
			"name":    pkg.Name,
			"version": depstemplate.NormalizeVersion(version),
			"tag":     tagName,
			"os":      plat.OS,
			"arch":    plat.Arch,
			"asset":   templatedPattern,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to template URL: %w", err)
		}
	} else {
		// Build GitHub release download URL from repo and asset pattern
		downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", pkg.Repo, tagName, templatedPattern)
	}

	resolution := &types.Resolution{
		Package:     pkg,
		Version:     version,
		Platform:    plat,
		DownloadURL: downloadURL,
		IsArchive:   isArchiveFile(downloadURL),
		// No checksum - rate limit fallback
	}

	if resolution.IsArchive {
		resolution.BinaryPath = m.guessBinaryPath(pkg, templatedPattern, plat)
	}

	logger.Debugf("Fallback resolved %s (no checksum)", resolution.Pretty().ANSI())
	return resolution, nil
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
	}
	return fmt.Sprintf("%ds", seconds)
}

// enhanceErrorWithVersions enhances version not found errors with available version suggestions
func (m *GitHubReleaseManager) enhanceErrorWithVersions(ctx context.Context, pkg types.Package, requestedVersion string, plat platform.Platform, originalErr error) error {
	versions, err := m.DiscoverVersions(ctx, pkg, plat, 20)
	if err != nil {
		return originalErr
	}
	return manager.EnhanceErrorWithVersions(pkg.Name, requestedVersion, versions, originalErr)
}

func (m *GitHubReleaseManager) guessBinaryPath(pkg types.Package, assetName string, plat platform.Platform) string {
	if pkg.BinaryPath != "" {
		data := map[string]any{
			"os":   plat.OS,
			"arch": plat.Arch,
			"name": pkg.Name,
		}

		result, err := depstemplate.EvaluateCELOrTemplate(pkg.BinaryPath, data)
		if err == nil && result != "" {
			return result
		}
		return pkg.BinaryPath
	}

	if pkg.BinaryName != "" {
		return pkg.BinaryName
	}

	baseName := pkg.Name
	if plat.IsWindows() {
		baseName += ".exe"
	}

	return baseName
}

func (m *GitHubReleaseManager) templateString(pattern string, data map[string]string) (string, error) {
	return depstemplate.TemplateString(pattern, data)
}

// stripChecksumPrefix removes the checksum type prefix (e.g., "sha256:") from a digest string
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
