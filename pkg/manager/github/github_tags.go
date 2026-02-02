package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/checksum"
	depshttp "github.com/flanksource/deps/pkg/http"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	depstemplate "github.com/flanksource/deps/pkg/template"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
	"github.com/google/go-github/v57/github"
)

// GitHubTagsManager implements the PackageManager interface for GitHub tags
type GitHubTagsManager struct {
	client *http.Client
}

// NewGitHubTagsManager creates a new GitHub tags manager.
func NewGitHubTagsManager() *GitHubTagsManager {
	return &GitHubTagsManager{
		client: depshttp.GetHttpClient(),
	}
}


// Name returns the manager identifier
func (m *GitHubTagsManager) Name() string {
	return "github_tags"
}

// DiscoverVersions returns the most recent versions from GitHub tags using git HTTP protocol.
// Falls back to GraphQL if git HTTP fails.
func (m *GitHubTagsManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	if pkg.Repo == "" {
		return nil, fmt.Errorf("repo is required for GitHub tags")
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
		return m.discoverVersionsViaREST(ctx, owner, repo, pkg, limit)
	}, opts)
	if err != nil {
		return nil, err
	}

	// Apply version expression filtering if specified
	if pkg.VersionExpr != "" {
		filteredVersions, err := version.ApplyVersionExpr(versions, pkg.VersionExpr)
		if err != nil {
			return nil, fmt.Errorf("failed to apply version_expr for %s: %w", pkg.Name, err)
		}
		versions = filteredVersions
		logger.V(4).Infof("GitHub Tags: After version_expr filtering: %d versions", len(versions))
	}

	// Filter out versions that are not valid semantic versions after transformation
	versions = version.FilterToValidSemver(versions)

	// Re-sort by transformed version (needed after version_expr transforms tags)
	version.SortVersions(versions)

	// Apply limit if specified
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}

	return versions, nil
}

// discoverVersionsViaREST fetches versions using GitHub REST API (fallback method)
func (m *GitHubTagsManager) discoverVersionsViaREST(ctx context.Context, owner, repo string, pkg types.Package, limit int) ([]types.Version, error) {
	perPage := limit
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}

	// Use REST API to get tags
	endpoint := fmt.Sprintf("/repos/%s/%s/tags?per_page=%d", owner, repo, perPage)
	var tags []restTag
	if err := GetClient().RESTRequest(ctx, "GET", endpoint, &tags); err != nil {
		err = m.enhanceRateLimitError(ctx, err)
		return nil, fmt.Errorf("failed to list tags for %s/%s: %w", owner, repo, err)
	}

	var versions []types.Version
	for _, tag := range tags {
		normalizedVersion := version.Normalize(tag.Name)
		v := types.ParseVersion(normalizedVersion, tag.Name)
		if tag.Commit != nil {
			v.SHA = tag.Commit.SHA
		}
		versions = append(versions, v)
	}

	logger.V(4).Infof("GitHub Tags: Found %d tags for %s/%s", len(versions), owner, repo)
	return versions, nil
}

// restTag represents a tag from REST API
type restTag struct {
	Name   string          `json:"name"`
	Commit *restTagCommit  `json:"commit"`
}

// restTagCommit represents a commit reference in a tag
type restTagCommit struct {
	SHA string `json:"sha"`
	URL string `json:"url"`
}

// Resolve gets the download URL and metadata for a specific version and platform
func (m *GitHubTagsManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	if pkg.URLTemplate == "" {
		return nil, fmt.Errorf("url_template is required for github_tags manager")
	}

	// Find the tag by version (pass full package for version_expr support)
	tag, err := m.findTagByVersion(ctx, pkg, version)
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

	// Get the asset pattern for this platform or use fallback
	assetPattern := ""

	// Use common asset pattern resolution
	if pkg.AssetPatterns != nil {
		assetPattern, _ = manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
		// Continue with empty assetPattern to fall back to default if error
	}

	// If no asset pattern found, use default pattern
	if assetPattern == "" {
		assetPattern = "{{.name}}-{{.os}}-{{.arch}}"
	}

	normalizedVersion := depstemplate.NormalizeVersion(version)

	// Template the asset pattern
	templatedPattern, err := m.templateString(assetPattern, map[string]string{
		"name":    pkg.Name,
		"version": normalizedVersion,
		"tag":     tag.Name,
		"os":      plat.OS,
		"arch":    plat.Arch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to template asset pattern: %w", err)
	}

	// Normalize URL template to auto-append {{.asset}} if it ends with /
	urlTemplate := manager.NormalizeURLTemplate(pkg.URLTemplate)

	// Template the URL using the URL template (mandatory for github_tags)
	downloadURL, err := m.templateString(urlTemplate, map[string]string{
		"name":    pkg.Name,
		"version": normalizedVersion,
		"tag":     tag.Name,
		"os":      plat.OS,
		"arch":    plat.Arch,
		"asset":   templatedPattern,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to template URL: %w", err)
	}

	// Check for JSON asset discovery
	var discoveredURL string
	var discoveredChecksum string

	if pkg.AssetsExpr != "" {
		// Download the URL to check if it's JSON
		resp, err := m.client.Get(downloadURL)
		if err == nil {
			defer func() { _ = resp.Body.Close() }()

			// Check Content-Type
			contentType := resp.Header.Get("Content-Type")
			if strings.Contains(contentType, "application/json") {
				// Read and parse JSON
				body, err := io.ReadAll(resp.Body)
				if err == nil {
					var jsonData interface{}
					if json.Unmarshal(body, &jsonData) == nil {
						// Build vars map for CEL evaluation
						vars := map[string]interface{}{
							"json":     jsonData,
							"os":       plat.OS,
							"arch":     plat.Arch,
							"version":  version,
							"package":  pkg,
							"platform": plat,
						}

						// Evaluate assets_expr to get URL and checksum
						// EvaluateCELExpression returns (checksumValue, checksumType, discoveredURL, error)
						checksumValue, checksumType, url, evalErr := checksum.EvaluateCELExpression(vars, pkg.AssetsExpr)
						if evalErr == nil {
							if url != "" {
								discoveredURL = url
							}
							if checksumValue != "" {
								// Format checksum with hash type prefix (e.g., "sha256:abc123")
								discoveredChecksum = checksum.FormatChecksum(checksumValue, checksumType)
							}
						}
					}
				}
			}
		}
	}

	// Use discovered URL if available, otherwise use templated URL
	finalDownloadURL := downloadURL
	if discoveredURL != "" {
		finalDownloadURL = discoveredURL
	}

	isArchive := isArchiveFile(finalDownloadURL)

	resolution := &types.Resolution{
		Package:     pkg,
		Version:     version,
		Platform:    plat,
		DownloadURL: finalDownloadURL,
		IsArchive:   isArchive,
	}

	// Set discovered checksum if available
	if discoveredChecksum != "" {
		resolution.Checksum = discoveredChecksum
	}

	// Set binary path for archives
	if resolution.IsArchive {
		resolution.BinaryPath = m.guessBinaryPath(pkg, templatedPattern, plat)
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
			"tag":     tag.Name,
		}

		evaluatedChecksumFile, err := depstemplate.EvaluateCELOrTemplate(checksumFile, data)
		if err == nil && evaluatedChecksumFile != "" {
			checksumFile = evaluatedChecksumFile
		}

		// Only proceed if we have a non-empty checksum file after evaluation
		if checksumFile != "" {
			checksumURL, err := m.templateChecksumURL(checksumFile, templatedPattern, version, tag.Name, plat)
			if err == nil && checksumURL != "" {
				resolution.ChecksumURL = checksumURL
			}
		}
	}

	return resolution, nil
}

// Install downloads and installs the binary
func (m *GitHubTagsManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	// For now, return not implemented - the actual installation
	// is handled by the existing deps.Install function
	return fmt.Errorf("install method not yet implemented - use existing Install")
}

// GetChecksums returns nil since github_tags uses url_template for downloads
// Checksums are managed externally via ChecksumFile field which points to external URLs
func (m *GitHubTagsManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	// github_tags manager uses url_template, not GitHub release assets
	// Checksum files are external URLs that are handled by the download/checksum package
	// No need to download separate checksum files here
	return nil, nil
}

// Verify checks if an installed binary matches expectations
func (m *GitHubTagsManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	// TODO: Implement verification logic
	return nil, fmt.Errorf("verify not implemented yet")
}

// WhoAmI returns authentication status and user information for GitHub
func (m *GitHubTagsManager) WhoAmI(ctx context.Context) *types.AuthStatus {
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
	status.HasPermissions = true // GitHub tags don't require special scopes

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

// enhanceRateLimitError wraps a rate limit error with current rate limit status
func (m *GitHubTagsManager) enhanceRateLimitError(ctx context.Context, originalErr error) error {
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

// handleRateLimitFallback handles rate limit errors by using fallback version
func (m *GitHubTagsManager) handleRateLimitFallback(ctx context.Context, pkg types.Package, ver string, plat platform.Platform, originalErr error) (*types.Resolution, error) {
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
	versionForTemplate := ver
	if ver == "latest" || ver == "" {
		versionForTemplate = fallbackVersion
	}

	// Build resolution without checksum
	return m.buildFallbackResolution(pkg, versionForTemplate, plat)
}

// hasNonWildcardAssetPattern checks if the package has an asset pattern for the platform
// that doesn't contain wildcards (can be used to construct a deterministic download URL)
func (m *GitHubTagsManager) hasNonWildcardAssetPattern(pkg types.Package, plat platform.Platform) bool {
	if pkg.AssetPatterns == nil {
		return false
	}
	pattern, err := manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
	if err != nil || pattern == "" {
		return false
	}
	return !strings.Contains(pattern, "*") && !strings.Contains(pattern, "?")
}

// buildFallbackResolution builds a resolution using url_template or asset_patterns without checksum
func (m *GitHubTagsManager) buildFallbackResolution(pkg types.Package, ver string, plat platform.Platform) (*types.Resolution, error) {
	// Get the asset pattern for this platform
	assetPattern, _ := manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
	if assetPattern == "" {
		assetPattern = "{{.name}}-{{.os}}-{{.arch}}"
	}

	// Determine tag name (typically "v" + version for GitHub tags)
	tagName := ver
	if !strings.HasPrefix(ver, "v") {
		tagName = "v" + ver
	}

	normalizedVersion := depstemplate.NormalizeVersion(ver)

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
			"version": normalizedVersion,
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
		Version:     ver,
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

// Helper methods

// TagInfo represents tag information from REST API
type TagInfo struct {
	Name string
	SHA  string
}

func (m *GitHubTagsManager) findTagByVersion(ctx context.Context, pkg types.Package, targetVersion string) (*TagInfo, error) {
	parts := strings.Split(pkg.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s", pkg.Repo)
	}
	owner, repo := parts[0], parts[1]
	logger.V(3).Infof("GitHub Tags: Searching for tag matching version %s in %s/%s using REST API", targetVersion, owner, repo)

	// Fetch tags via REST API
	endpoint := fmt.Sprintf("/repos/%s/%s/tags?per_page=100", owner, repo)
	var tags []restTag
	if err := GetClient().RESTRequest(ctx, "GET", endpoint, &tags); err != nil {
		err = m.enhanceRateLimitError(ctx, err)
		return nil, fmt.Errorf("failed to query tags: %w", err)
	}

	// Try exact tag match first (with version_expr transformation)
	for _, tag := range tags {
		tagName := tag.Name
		transformedTag := tagName
		if pkg.VersionExpr != "" {
			transformed, err := ApplyVersionExprToSingleTag(tagName, pkg.VersionExpr)
			if err == nil && transformed != "" {
				transformedTag = transformed
			}
		}

		if transformedTag == targetVersion || transformedTag == "v"+targetVersion {
			sha := ""
			if tag.Commit != nil {
				sha = tag.Commit.SHA
			}
			return &TagInfo{
				Name: transformedTag,
				SHA:  sha,
			}, nil
		}
	}

	// Try version normalization match (with version_expr transformation)
	normalizedTarget := version.Normalize(targetVersion)
	for _, tag := range tags {
		tagName := tag.Name
		transformedTag := tagName
		if pkg.VersionExpr != "" {
			transformed, err := ApplyVersionExprToSingleTag(tagName, pkg.VersionExpr)
			if err == nil && transformed != "" {
				transformedTag = transformed
			}
		}

		if version.Normalize(transformedTag) == normalizedTarget {
			sha := ""
			if tag.Commit != nil {
				sha = tag.Commit.SHA
			}
			return &TagInfo{
				Name: transformedTag,
				SHA:  sha,
			}, nil
		}
	}

	return nil, &manager.ErrVersionNotFound{
		Package: repo,
		Version: targetVersion,
	}
}

// ApplyVersionExprToSingleTag applies a version expression to a single tag
// Returns the transformed tag or original tag if transformation fails/empty
func ApplyVersionExprToSingleTag(tag, versionExpr string) (string, error) {
	if versionExpr == "" {
		return tag, nil
	}

	// Create a test version with the tag
	testVersion := types.Version{
		Tag:     tag,
		Version: tag,
	}

	// Apply the version expression
	result, err := version.ApplyVersionExpr([]types.Version{testVersion}, versionExpr)
	if err != nil {
		return "", fmt.Errorf("failed to apply version_expr to tag %s: %w", tag, err)
	}

	// If no results or empty result, return empty (excluded)
	if len(result) == 0 {
		return "", nil
	}

	return result[0].Tag, nil
}

func (m *GitHubTagsManager) templateString(pattern string, data map[string]string) (string, error) {
	return depstemplate.TemplateString(pattern, data)
}

// enhanceErrorWithVersions enhances version not found errors with available version suggestions
func (m *GitHubTagsManager) enhanceErrorWithVersions(ctx context.Context, pkg types.Package, requestedVersion string, plat platform.Platform, originalErr error) error {
	// Try to get available versions using a default platform for error enhancement
	versions, err := m.DiscoverVersions(ctx, pkg, plat, 20)
	if err != nil {
		// If we can't get versions, return the original error
		return originalErr
	}

	return manager.EnhanceErrorWithVersions(pkg.Name, requestedVersion, versions, originalErr)
}

func (m *GitHubTagsManager) templateChecksumURL(checksumPattern, assetName, version, tag string, plat platform.Platform) (string, error) {
	// Template the checksum URL
	url, err := m.templateString(checksumPattern, map[string]string{
		"version": depstemplate.NormalizeVersion(version),
		"tag":     tag,
		"os":      plat.OS,
		"arch":    plat.Arch,
		"asset":   assetName,
	})
	if err != nil {
		return "", err
	}

	return url, nil
}

func (m *GitHubTagsManager) guessBinaryPath(pkg types.Package, assetName string, plat platform.Platform) string {
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
