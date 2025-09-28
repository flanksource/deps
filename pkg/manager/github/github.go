package github

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/deps/pkg/checksum"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	depstemplate "github.com/flanksource/deps/pkg/template"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

// GitHubReleaseManager implements the PackageManager interface for GitHub releases
type GitHubReleaseManager struct {
	client      *github.Client
	tokenSource string
}

// NewGitHubReleaseManager creates a new GitHub release manager
func NewGitHubReleaseManager(token, tokenSource string) *GitHubReleaseManager {
	var client *github.Client

	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		tc := oauth2.NewClient(context.Background(), ts)
		client = github.NewClient(tc)
	} else {
		client = github.NewClient(nil)
	}

	return &GitHubReleaseManager{
		client:      client,
		tokenSource: tokenSource,
	}
}

// Name returns the manager identifier
func (m *GitHubReleaseManager) Name() string {
	return "github_release"
}

// DiscoverVersions returns the most recent versions from GitHub releases
func (m *GitHubReleaseManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	if pkg.Repo == "" {
		return nil, fmt.Errorf("repo is required for GitHub releases")
	}

	parts := strings.Split(pkg.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s (expected owner/repo)", pkg.Repo)
	}
	owner, repo := parts[0], parts[1]

	// Set appropriate page size based on limit
	perPage := limit
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}

	releases, _, err := m.client.Repositories.ListReleases(ctx, owner, repo, &github.ListOptions{
		PerPage: perPage,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list releases for %s: %w", pkg.Repo, err)
	}

	var versions []types.Version
	for _, release := range releases {
		if release.TagName == nil {
			continue
		}

		version := types.Version{
			Tag:        *release.TagName,
			Version:    version.Normalize(*release.TagName),
			Prerelease: release.Prerelease != nil && *release.Prerelease,
		}

		if release.PublishedAt != nil {
			version.Published = release.PublishedAt.Time
		}
		if release.TargetCommitish != nil {
			version.SHA = *release.TargetCommitish
		}

		versions = append(versions, version)
	}

	// Apply version expression filtering if specified
	if pkg.VersionExpr != "" {
		filteredVersions, err := version.ApplyVersionExpr(versions, pkg.VersionExpr)
		if err != nil {
			return nil, fmt.Errorf("failed to apply version_expr for %s: %w", pkg.Name, err)
		}
		versions = filteredVersions
	}

	// Sort versions in descending order (newest first)
	sort.Slice(versions, func(i, j int) bool {
		v1, err1 := semver.NewVersion(versions[i].Version)
		v2, err2 := semver.NewVersion(versions[j].Version)

		if err1 != nil || err2 != nil {
			// Fallback to string comparison
			return versions[i].Version > versions[j].Version
		}

		return v1.GreaterThan(v2)
	})

	// Apply limit if specified
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}

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

	// Find the release by version/tag
	release, err := m.findReleaseByVersion(ctx, owner, repo, version)
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

	// First try to get asset pattern from AssetPatterns (with wildcard support)
	if pkg.AssetPatterns != nil {
		fmt.Printf("DEBUG: Looking for asset pattern for platform: %s\n", platformKey)
		fmt.Printf("DEBUG: Available asset patterns: %+v\n", pkg.AssetPatterns)
		assetPattern = findAssetPatternForPlatform(pkg.AssetPatterns, platformKey)
		fmt.Printf("DEBUG: Selected asset pattern: %s\n", assetPattern)
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

	// Template the asset pattern
	templateData := map[string]string{
		"name":    pkg.Name,
		"version": depstemplate.NormalizeVersion(version),
		"tag":     *release.TagName,
		"os":      plat.OS,
		"arch":    plat.Arch,
	}
	fmt.Printf("DEBUG: Template data: %+v\n", templateData)
	fmt.Printf("DEBUG: Raw asset pattern: %s\n", assetPattern)

	templatedPattern, err := m.templateString(assetPattern, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to template asset pattern: %w", err)
	}
	fmt.Printf("DEBUG: Templated asset pattern: %s\n", templatedPattern)

	// Debug: GitHub asset pattern templated: %s -> %s

	var downloadURL string
	var isArchive bool
	var githubAsset *types.GitHubAsset

	// Check if the templated pattern itself is a URL (URL override)
	if hasURLSchema(templatedPattern) {
		// The asset pattern contains a direct URL, use it
		downloadURL = templatedPattern
		isArchive = isArchiveFile(templatedPattern)

		// Debug: GitHub using direct URL from asset pattern: %s
	} else if pkg.URLTemplate != "" {
		// Debug: GitHub using URL template: %s

		// Use the URL template instead of GitHub release assets
		downloadURL, err = m.templateString(pkg.URLTemplate, map[string]string{
			"name":    pkg.Name,
			"version": depstemplate.NormalizeVersion(version), // normalized without "v" prefix
			"tag":     *release.TagName,
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
		// Find the matching asset in GitHub release
		fmt.Printf("DEBUG: Searching for asset pattern '%s' in %d release assets\n", templatedPattern, len(release.Assets))
		fmt.Printf("DEBUG: Available assets:\n")
		for i, asset := range release.Assets {
			if asset.Name != nil {
				fmt.Printf("DEBUG:   [%d] %s\n", i, *asset.Name)
			}
		}

		var matchedAsset *github.ReleaseAsset
		for _, asset := range release.Assets {
			if asset.Name != nil && *asset.Name == templatedPattern {
				fmt.Printf("DEBUG: Found matching asset: %s\n", *asset.Name)
				matchedAsset = asset
				break
			}
		}

		if matchedAsset == nil {
			fmt.Printf("DEBUG: No matching asset found for pattern: %s\n", templatedPattern)
			// Extract available asset names for enhanced error
			availableAssets := make([]string, 0, len(release.Assets))
			for _, asset := range release.Assets {
				if asset.Name != nil {
					availableAssets = append(availableAssets, *asset.Name)
				}
			}

			// Create enhanced asset not found error
			assetErr := &manager.ErrAssetNotFound{
				Package:         pkg.Name,
				AssetPattern:    templatedPattern,
				Platform:        platformKey,
				AvailableAssets: availableAssets,
			}

			// Enhance the error with available assets and suggestions
			return nil, manager.EnhanceAssetNotFoundError(pkg.Name, templatedPattern, platformKey, availableAssets, assetErr)
		}

		// Debug: GitHub found matching asset: %s

		downloadURL = *matchedAsset.BrowserDownloadURL
		isArchive = isArchiveFile(*matchedAsset.Name)
		githubAsset = &types.GitHubAsset{
			Repo:        pkg.Repo,
			Tag:         *release.TagName,
			AssetName:   *matchedAsset.Name,
			AssetID:     *matchedAsset.ID,
			DownloadURL: *matchedAsset.BrowserDownloadURL,
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

	// Template checksum URL if available
	if pkg.ChecksumFile != "" {
		// First evaluate ChecksumFile as CEL if it looks like a CEL expression
		checksumFile := pkg.ChecksumFile
		data := map[string]interface{}{
			"os":      plat.OS,
			"arch":    plat.Arch,
			"name":    pkg.Name,
			"version": depstemplate.NormalizeVersion(version),
			"tag":     *release.TagName,
		}

		evaluatedChecksumFile, err := depstemplate.EvaluateCELOrTemplate(checksumFile, data)
		if err == nil && evaluatedChecksumFile != "" {
			checksumFile = evaluatedChecksumFile
		}

		// Only proceed if we have a non-empty checksum file after evaluation
		if checksumFile != "" {
			assetName := templatedPattern
			if githubAsset != nil {
				assetName = githubAsset.AssetName
			}

			checksumURL, err := m.templateChecksumURL(checksumFile, assetName, version, *release.TagName, plat, release)
			if err == nil && checksumURL != "" {
				resolution.ChecksumURL = checksumURL
			}
		}
	}

	return resolution, nil
}

// Install downloads and installs the binary
func (m *GitHubReleaseManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	// For now, return not implemented - the actual installation
	// is handled by the existing deps.Install function
	// TODO: Implement proper installation using download package
	return fmt.Errorf("install method not yet implemented - use existing Install")
}

// GetChecksums retrieves checksums for all platforms
func (m *GitHubReleaseManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	if pkg.ChecksumFile == "" {
		return nil, fmt.Errorf("no checksum file pattern specified for package %s", pkg.Name)
	}

	parts := strings.Split(pkg.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s", pkg.Repo)
	}
	owner, repo := parts[0], parts[1]

	release, err := m.findReleaseByVersion(ctx, owner, repo, version)
	if err != nil {
		return nil, err
	}

	checksumURL := m.findChecksumURL(release, pkg.ChecksumFile, version, *release.TagName)
	if checksumURL == "" {
		return nil, fmt.Errorf("checksum file not found for version %s", version)
	}

	// Download and parse checksum file
	return m.downloadAndParseChecksums(ctx, checksumURL)
}

// Verify checks if an installed binary matches expectations
func (m *GitHubReleaseManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	// TODO: Implement verification logic
	return nil, fmt.Errorf("verify not implemented yet")
}

// WhoAmI returns authentication status and user information for GitHub
func (m *GitHubReleaseManager) WhoAmI(ctx context.Context) *types.AuthStatus {
	status := &types.AuthStatus{
		Service:     "GitHub",
		TokenSource: m.tokenSource,
	}

	// Get authenticated user information
	user, response, err := m.client.Users.Get(ctx, "")
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

// Helper methods

func (m *GitHubReleaseManager) findReleaseByVersion(ctx context.Context, owner, repo, targetVersion string) (*github.RepositoryRelease, error) {
	fmt.Printf("DEBUG: GitHub fetching releases for %s/%s, looking for version: %s\n", owner, repo, targetVersion)

	releases, _, err := m.client.Repositories.ListReleases(ctx, owner, repo, &github.ListOptions{
		PerPage: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}

	fmt.Printf("DEBUG: GitHub found %d releases, checking for version %s\n", len(releases), targetVersion)
	for i, release := range releases {
		if release.TagName != nil {
			fmt.Printf("DEBUG:   [%d] Tag: %s\n", i, *release.TagName)
		}
		if i >= 5 { // Show first 5 for brevity
			fmt.Printf("DEBUG:   ... and %d more releases\n", len(releases)-6)
			break
		}
	}

	// Try exact tag match first
	fmt.Printf("DEBUG: Trying exact tag match for: %s or v%s\n", targetVersion, targetVersion)
	for _, release := range releases {
		if release.TagName != nil && (*release.TagName == targetVersion || *release.TagName == "v"+targetVersion) {
			fmt.Printf("DEBUG: Found exact tag match: %s\n", *release.TagName)
			return release, nil
		}
	}

	// Try version normalization match
	normalizedTarget := version.Normalize(targetVersion)
	for _, release := range releases {
		if release.TagName != nil && version.Normalize(*release.TagName) == normalizedTarget {
			return release, nil
		}
	}

	return nil, &manager.ErrVersionNotFound{
		Package: repo,
		Version: targetVersion,
	}
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

func (m *GitHubReleaseManager) findChecksumURL(release *github.RepositoryRelease, checksumPattern, version, tag string) string {
	templatedName, err := m.templateString(checksumPattern, map[string]string{
		"version": depstemplate.NormalizeVersion(version),
		"tag":     tag,
	})
	if err != nil {
		return ""
	}

	for _, asset := range release.Assets {
		if asset.Name != nil && *asset.Name == templatedName {
			return *asset.BrowserDownloadURL
		}
	}

	return ""
}

func (m *GitHubReleaseManager) templateChecksumURL(checksumPattern, assetName, version, tag string, plat platform.Platform, release *github.RepositoryRelease) (string, error) {
	// Handle comma-separated checksum files
	checksumPatterns := strings.Split(checksumPattern, ",")
	var checksumURLs []string

	for _, pattern := range checksumPatterns {
		pattern = strings.TrimSpace(pattern)

		// Check if checksum pattern is a full URL template (starts with http/https)
		if strings.HasPrefix(pattern, "http://") || strings.HasPrefix(pattern, "https://") {
			// Template the full URL
			url, err := m.templateString(pattern, map[string]string{
				"version": depstemplate.NormalizeVersion(version),
				"tag":     tag,
				"os":      plat.OS,
				"arch":    plat.Arch,
				"asset":   assetName,
			})
			if err != nil {
				return "", err
			}
			checksumURLs = append(checksumURLs, url)
		} else {
			// For checksum files (not full URLs), look for the file in GitHub release assets
			templatedChecksumFile, err := m.templateString(pattern, map[string]string{
				"version": depstemplate.NormalizeVersion(version),
				"tag":     tag,
				"os":      plat.OS,
				"arch":    plat.Arch,
				"asset":   assetName,
			})
			if err != nil {
				return "", err
			}

			// Find matching checksum file in release assets
			found := false
			for _, asset := range release.Assets {
				if asset.Name != nil && *asset.Name == templatedChecksumFile {
					checksumURLs = append(checksumURLs, *asset.BrowserDownloadURL)
					found = true
					break
				}
			}

			if !found {
				return "", fmt.Errorf("checksum file %s not found in release assets", templatedChecksumFile)
			}
		}
	}

	// Return comma-separated URLs
	return strings.Join(checksumURLs, ","), nil
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

func (m *GitHubReleaseManager) downloadAndParseChecksums(ctx context.Context, url string) (map[string]string, error) {
	// Use the checksum package's discovery mechanisms
	discovery := checksum.NewDiscovery()

	// Create a minimal resolution for the discovery
	resolution := &types.Resolution{
		ChecksumURL: url,
	}

	return discovery.FindChecksums(ctx, resolution)
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

// matchPlatformPattern checks if a platform matches a wildcard pattern
// Supports comma-separated patterns like "darwin-*,windows-*"
func matchPlatformPattern(pattern string, platform string) bool {
	patterns := strings.Split(pattern, ",")
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if matched, _ := filepath.Match(p, platform); matched {
			return true
		}
	}
	return false
}

// findAssetPatternForPlatform finds the best matching asset pattern for a platform
// First tries exact match, then tries wildcard patterns
func findAssetPatternForPlatform(assetPatterns map[string]string, platform string) string {
	// First try exact match
	if pattern, exists := assetPatterns[platform]; exists {
		return pattern
	}

	// Try wildcard patterns
	for patternKey, patternValue := range assetPatterns {
		if strings.Contains(patternKey, "*") || strings.Contains(patternKey, ",") {
			if matchPlatformPattern(patternKey, platform) {
				return patternValue
			}
		}
	}

	return ""
}
