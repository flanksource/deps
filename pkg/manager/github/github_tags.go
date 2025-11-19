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
	"github.com/shurcooL/githubv4"
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

// GraphQL query structs for tags with commit dates
type tagsQuery struct {
	Repository struct {
		Refs struct {
			Nodes []struct {
				Name   string
				Target struct {
					Tag struct {
						Target struct {
							Commit struct {
								Oid           string
								CommittedDate time.Time
							} `graphql:"... on Commit"`
						}
					} `graphql:"... on Tag"`
					Commit struct {
						Oid           string
						CommittedDate time.Time
					} `graphql:"... on Commit"`
				}
			}
			PageInfo struct {
				HasNextPage bool
				EndCursor   githubv4.String
			}
		} `graphql:"refs(refPrefix: \"refs/tags/\", first: $first, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}, after: $after)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// Name returns the manager identifier
func (m *GitHubTagsManager) Name() string {
	return "github_tags"
}

// DiscoverVersions returns the most recent versions from GitHub tags
func (m *GitHubTagsManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	if pkg.Repo == "" {
		return nil, fmt.Errorf("repo is required for GitHub tags")
	}

	parts := strings.Split(pkg.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s (expected owner/repo)", pkg.Repo)
	}
	owner, repo := parts[0], parts[1]

	// Set appropriate page size based on limit
	first := limit
	if first <= 0 || first > 100 {
		first = 100
	}

	// Execute GraphQL query for tags sorted by commit date
	graphql := GetClient().GraphQL()
	var query tagsQuery
	variables := map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(repo),
		"first": githubv4.Int(first),
		"after": (*githubv4.String)(nil),
	}

	err := graphql.Query(ctx, &query, variables)
	if err != nil {
		err = m.enhanceRateLimitError(ctx, err)
		return nil, fmt.Errorf("failed to query tags for %s: %w", pkg.Repo, err)
	}

	// Extract tag information from GraphQL response
	var versions []types.Version
	var tagNames []string

	for _, node := range query.Repository.Refs.Nodes {
		tagName := node.Name
		tagNames = append(tagNames, tagName)

		// Extract commit SHA and date
		var commitSHA string
		var commitDate time.Time

		// Handle both Tag -> Commit and direct Commit references
		if node.Target.Tag.Target.Commit.Oid != "" {
			// Tag points to another tag that points to a commit
			commitSHA = node.Target.Tag.Target.Commit.Oid
			commitDate = node.Target.Tag.Target.Commit.CommittedDate
		} else if node.Target.Commit.Oid != "" {
			// Tag points directly to a commit
			commitSHA = node.Target.Commit.Oid
			commitDate = node.Target.Commit.CommittedDate
		}

		normalizedVersion := version.Normalize(tagName)
		isPrerelease := version.IsPrerelease(normalizedVersion)

		versions = append(versions, types.Version{
			Tag:        tagName,
			Version:    normalizedVersion,
			SHA:        commitSHA,
			Published:  commitDate,
			Prerelease: isPrerelease,
		})

	}

	// Log all tags returned from GraphQL API at V(4) level
	logger.V(4).Infof("GitHub Tags: Found %d total tags for %s", len(tagNames), pkg.Repo)
	logger.V(4).Infof("GitHub Tags: Found %d valid semantic version tags", len(versions))
	logger.V(4).Infof("GitHub Tags: Raw tag names: %v", tagNames)

	// Apply version expression filtering if specified
	if pkg.VersionExpr != "" {
		filteredVersions, err := version.ApplyVersionExpr(versions, pkg.VersionExpr)
		if err != nil {
			return nil, fmt.Errorf("failed to apply version_expr for %s: %w", pkg.Name, err)
		}
		versions = filteredVersions
		logger.V(4).Infof("GitHub Tags: After version_expr filtering: %d versions", len(versions))
	}

	// Tags are already sorted by commit date (DESC) from GraphQL query
	// and filtered to only valid semantic versions

	// Apply limit if specified (should not be needed as GraphQL query handles this)
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}

	return versions, nil
}

// Resolve gets the download URL and metadata for a specific version and platform
func (m *GitHubTagsManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	if pkg.URLTemplate == "" {
		return nil, fmt.Errorf("url_template is required for github_tags manager")
	}

	// Find the tag by version (pass full package for version_expr support)
	tag, err := m.findTagByVersion(ctx, pkg, version)
	if err != nil {
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
		var err error
		assetPattern, err = manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
		if err != nil {
			// Continue with empty assetPattern to fall back to default
		}
	}

	// If no asset pattern found, use default pattern
	if assetPattern == "" {
		assetPattern = "{{.name}}-{{.os}}-{{.arch}}"
	}

	// Template the asset pattern
	templatedPattern, err := m.templateString(assetPattern, map[string]string{
		"name":               pkg.Name,
		"version":            version,
		"normalized_version": depstemplate.NormalizeVersion(version),
		"tag":                tag.Name,
		"os":                 plat.OS,
		"arch":               plat.Arch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to template asset pattern: %w", err)
	}

	// Normalize URL template to auto-append {{.asset}} if it ends with /
	urlTemplate := manager.NormalizeURLTemplate(pkg.URLTemplate)

	// Template the URL using the URL template (mandatory for github_tags)
	downloadURL, err := m.templateString(urlTemplate, map[string]string{
		"name":               pkg.Name,
		"version":            version,
		"normalized_version": depstemplate.NormalizeVersion(version),
		"tag":                tag.Name,
		"os":                 plat.OS,
		"arch":               plat.Arch,
		"asset":              templatedPattern,
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
			defer resp.Body.Close()

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

// Helper methods

// TagInfo represents tag information from GraphQL query
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
	// Log GraphQL query initiation
	logger.V(3).Infof("GitHub Tags: Searching for tag matching version %s in %s/%s using GraphQL", targetVersion, owner, repo)
	logger.V(4).Infof("GitHub Tags: Tag search GraphQL query - owner: %s, repo: %s, first: 100", owner, repo)

	// Execute GraphQL query for tags (we need to search through them)
	graphql := GetClient().GraphQL()
	var query tagsQuery
	variables := map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(repo),
		"first": githubv4.Int(100),
		"after": (*githubv4.String)(nil),
	}

	err := graphql.Query(ctx, &query, variables)
	if err != nil {
		err = m.enhanceRateLimitError(ctx, err)
		return nil, fmt.Errorf("failed to query tags: %w", err)
	}

	// Try exact tag match first (with version_expr transformation)
	for _, node := range query.Repository.Refs.Nodes {
		tagName := node.Name

		// Apply version_expr transformation if specified
		transformedTag := tagName
		if pkg.VersionExpr != "" {
			transformed, err := ApplyVersionExprToSingleTag(tagName, pkg.VersionExpr)
			if err == nil && transformed != "" {
				transformedTag = transformed
			}
		}

		// Check if transformed tag matches target version
		if transformedTag == targetVersion || transformedTag == "v"+targetVersion {
			// Extract commit SHA
			var commitSHA string
			if node.Target.Tag.Target.Commit.Oid != "" {
				commitSHA = node.Target.Tag.Target.Commit.Oid
			} else if node.Target.Commit.Oid != "" {
				commitSHA = node.Target.Commit.Oid
			}

			return &TagInfo{
				Name: transformedTag, // Use transformed tag for URL templating
				SHA:  commitSHA,
			}, nil
		}
	}

	// Try version normalization match (with version_expr transformation)
	normalizedTarget := version.Normalize(targetVersion)
	for _, node := range query.Repository.Refs.Nodes {
		tagName := node.Name

		// Apply version_expr transformation if specified
		transformedTag := tagName
		if pkg.VersionExpr != "" {
			transformed, err := ApplyVersionExprToSingleTag(tagName, pkg.VersionExpr)
			if err == nil && transformed != "" {
				transformedTag = transformed
			}
		}

		if version.Normalize(transformedTag) == normalizedTarget {
			// Extract commit SHA
			var commitSHA string
			if node.Target.Tag.Target.Commit.Oid != "" {
				commitSHA = node.Target.Tag.Target.Commit.Oid
			} else if node.Target.Commit.Oid != "" {
				commitSHA = node.Target.Commit.Oid
			}

			return &TagInfo{
				Name: transformedTag, // Use transformed tag for URL templating
				SHA:  commitSHA,
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
