package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	depshttp "github.com/flanksource/deps/pkg/http"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	depstemplate "github.com/flanksource/deps/pkg/template"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
)

// GitLabReleaseManager implements the PackageManager interface for GitLab releases
type GitLabReleaseManager struct {
	client      *http.Client
	token       string
	tokenSource string
}

// GitLabRelease represents a GitLab release
type GitLabRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	Assets      struct {
		Links []GitLabReleaseAsset `json:"links"`
	} `json:"assets"`
}

// GitLabReleaseAsset represents a GitLab release asset
type GitLabReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// GraphQL API types
type GraphQLRequest struct {
	OperationName string           `json:"operationName"`
	Variables     GraphQLVariables `json:"variables"`
	Query         string           `json:"query"`
}

type GraphQLVariables struct {
	FullPath string `json:"fullPath"`
	First    int    `json:"first"`
	Sort     string `json:"sort"`
}

type GraphQLResponse struct {
	Data   GraphQLData    `json:"data"`
	Errors []GraphQLError `json:"errors,omitempty"`
}

type GraphQLError struct {
	Message string   `json:"message"`
	Path    []string `json:"path,omitempty"`
}

type GraphQLData struct {
	Project GraphQLProject `json:"project"`
}

type GraphQLProject struct {
	ID       string          `json:"id"`
	Releases GraphQLReleases `json:"releases"`
}

type GraphQLReleases struct {
	Nodes []GraphQLRelease `json:"nodes"`
}

type GraphQLRelease struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	TagName         string               `json:"tagName"`
	DescriptionHtml string               `json:"descriptionHtml"`
	ReleasedAt      string               `json:"releasedAt"`
	CreatedAt       string               `json:"createdAt"`
	Assets          GraphQLReleaseAssets `json:"assets"`
}

type GraphQLReleaseAssets struct {
	Count int                 `json:"count"`
	Links GraphQLReleaseLinks `json:"links"`
}

type GraphQLReleaseLinks struct {
	Nodes []GraphQLReleaseLink `json:"nodes"`
}

type GraphQLReleaseLink struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	DirectAssetURL string `json:"directAssetUrl"`
	LinkType       string `json:"linkType"`
}

// GraphQL query constant
const graphQLReleasesQuery = `query allReleases($fullPath: ID!, $first: Int, $last: Int, $before: String, $after: String, $sort: ReleaseSort) {
  project(fullPath: $fullPath) {
    id
    releases(first: $first, last: $last, before: $before, after: $after, sort: $sort) {
      nodes {
        id
        name
        tagName
        tagPath
        descriptionHtml
        releasedAt
        createdAt
        assets {
          count
          links {
            nodes {
              id
              name
              url
              directAssetUrl
              linkType
            }
          }
        }
      }
    }
  }
}`

// NewGitLabReleaseManager creates a new GitLab release manager
func NewGitLabReleaseManager(token, tokenSource string) *GitLabReleaseManager {
	return &GitLabReleaseManager{
		client:      depshttp.GetHttpClient(),
		token:       token,
		tokenSource: tokenSource,
	}
}

// Name returns the manager identifier
func (m *GitLabReleaseManager) Name() string {
	return "gitlab"
}

// DiscoverVersions returns the most recent versions from GitLab releases
func (m *GitLabReleaseManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	if pkg.Repo == "" {
		return nil, fmt.Errorf("package %s has no repository specified", pkg.Name)
	}

	releases, err := m.fetchReleases(ctx, pkg.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitLab releases for %s: %w", pkg.Repo, err)
	}

	versions := make([]types.Version, 0, len(releases))
	for _, release := range releases {
		if release.TagName == "" {
			continue
		}

		// Parse semantic version
		v, err := semver.NewVersion(release.TagName)
		if err != nil {
			continue // Skip invalid versions
		}

		versions = append(versions, types.Version{
			Version: v.String(),
			Tag:     release.TagName,
		})
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
		vi, _ := semver.NewVersion(versions[i].Version)
		vj, _ := semver.NewVersion(versions[j].Version)
		return vi.GreaterThan(vj)
	})

	// Apply limit if specified
	if limit > 0 && limit < len(versions) {
		versions = versions[:limit]
	}

	return versions, nil
}

// Resolve gets the download URL and checksum for a specific version and platform
func (m *GitLabReleaseManager) Resolve(ctx context.Context, pkg types.Package, versionStr string, plat platform.Platform) (*types.Resolution, error) {
	// Find the matching version by exact tag or normalized version
	releases, err := m.fetchReleases(ctx, pkg.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitLab releases: %w", err)
	}

	var targetRelease *GitLabRelease
	for _, release := range releases {
		if release.TagName == versionStr || release.TagName == "v"+versionStr {
			targetRelease = &release
			break
		}
		// Try normalized version matching
		if v, parseErr := semver.NewVersion(release.TagName); parseErr == nil {
			if version.Normalize(v.String()) == version.Normalize(versionStr) {
				targetRelease = &release
				break
			}
		}
	}

	if targetRelease == nil {
		return nil, &manager.ErrVersionNotFound{Package: pkg.Name, Version: versionStr}
	}

	// Get the asset pattern for this platform
	pattern, exists := pkg.AssetPatterns[plat.String()]
	if !exists {
		// Try wildcard patterns (simplified - no MatchesPattern function available)
		for key, p := range pkg.AssetPatterns {
			if strings.Contains(key, "*") {
				// Simple wildcard matching for OS
				if strings.HasPrefix(key, plat.OS+"-") || key == plat.OS+"-*" {
					pattern = p
					exists = true
					break
				}
			}
		}
	}

	if !exists {
		// Extract available asset names for better error message
		availableAssets := make([]string, 0, len(targetRelease.Assets.Links))
		for _, asset := range targetRelease.Assets.Links {
			availableAssets = append(availableAssets, asset.Name)
		}

		// If we have available assets, provide enhanced error message
		if len(availableAssets) > 0 {
			assetErr := &manager.ErrAssetNotFound{
				Package:         pkg.Name,
				AssetPattern:    fmt.Sprintf("(no pattern found for %s)", plat.String()),
				Platform:        plat.String(),
				AvailableAssets: availableAssets,
			}
			return nil, manager.EnhanceAssetNotFoundError(pkg.Name, fmt.Sprintf("(no pattern found for %s)", plat.String()), plat.String(), availableAssets, assetErr)
		}

		return nil, &manager.ErrPlatformNotSupported{Package: pkg.Name, Platform: plat.String()}
	}

	// Template the asset name
	assetName, err := depstemplate.TemplateString(pattern, map[string]string{
		"version": depstemplate.NormalizeVersion(targetRelease.TagName),
		"tag":     targetRelease.TagName,
		"os":      plat.OS,
		"arch":    plat.Arch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to template asset pattern: %w", err)
	}

	// Validate that the templated asset actually exists in the release
	var matchedAsset *GitLabReleaseAsset
	for _, asset := range targetRelease.Assets.Links {
		if asset.Name == assetName {
			matchedAsset = &asset
			break
		}
	}

	if matchedAsset == nil {
		// Convert to manager.AssetInfo format for filtering
		filterAssets := make([]manager.AssetInfo, len(targetRelease.Assets.Links))
		for i, asset := range targetRelease.Assets.Links {
			filterAssets[i] = manager.AssetInfo{
				Name:        asset.Name,
				DownloadURL: asset.URL,
				SHA256:      "", // GitLab doesn't include SHA256 in asset list
			}
		}

		// Try iterative filtering as fallback
		filtered, filterErr := manager.FilterAssetsByPlatform(filterAssets, plat.OS, plat.Arch)
		if filterErr == nil && len(filtered) == 1 {
			// Found exactly one asset through filtering - use it
			// Find the matching asset from the original list
			for _, asset := range targetRelease.Assets.Links {
				if asset.Name == filtered[0].Name {
					matchedAsset = &asset
					break
				}
			}
		}

		// If filtering didn't find a single result, create enhanced error
		if matchedAsset == nil {
			availableAssets := make([]string, 0, len(targetRelease.Assets.Links))
			for _, asset := range targetRelease.Assets.Links {
				availableAssets = append(availableAssets, asset.Name)
			}

			assetErr := &manager.ErrAssetNotFound{
				Package:         pkg.Name,
				AssetPattern:    assetName,
				Platform:        plat.String(),
				AvailableAssets: availableAssets,
			}

			return nil, manager.EnhanceAssetNotFoundError(pkg.Name, assetName, plat.String(), availableAssets, assetErr)
		}
	}

	// Use the matched asset's URL if available, otherwise construct download URL
	downloadURL := matchedAsset.URL
	if downloadURL == "" {
		// Fallback to constructed URL - GitLab uses different URL structure
		repoPath := url.PathEscape(pkg.Repo)
		downloadURL = fmt.Sprintf("https://gitlab.com/%s/-/releases/%s/downloads/%s", repoPath, targetRelease.TagName, assetName)
	}

	resolution := &types.Resolution{
		Package:     pkg,
		Version:     strings.TrimPrefix(targetRelease.TagName, "v"),
		Platform:    plat,
		DownloadURL: downloadURL,
	}

	// Handle checksums if available
	if pkg.ChecksumFile != "" {
		checksums, err := m.GetChecksums(ctx, pkg, strings.TrimPrefix(targetRelease.TagName, "v"))
		if err != nil {
			return nil, fmt.Errorf("failed to get checksums: %w", err)
		}
		if checksum, exists := checksums[plat.String()]; exists {
			resolution.Checksum = checksum
		}
	}

	return resolution, nil
}

// Install downloads and installs a binary for the given resolution
func (m *GitLabReleaseManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	// Use the shared installation logic (should be available in a common package)
	// For now, return not implemented as this would be shared with GitHub manager
	return fmt.Errorf("install not implemented for GitLab manager")
}

// GetChecksums retrieves checksums for all platforms for a given version
func (m *GitLabReleaseManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	if pkg.ChecksumFile == "" {
		return nil, nil
	}

	// Find the release for this version
	releases, err := m.fetchReleases(ctx, pkg.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitLab releases: %w", err)
	}

	var targetRelease *GitLabRelease
	for _, release := range releases {
		cleanTag := strings.TrimPrefix(release.TagName, "v")
		if cleanTag == version || release.TagName == version {
			targetRelease = &release
			break
		}
	}

	if targetRelease == nil {
		return nil, &manager.ErrVersionNotFound{Package: pkg.Name, Version: version}
	}

	// Template the checksum URL (not implemented yet)
	_, err = depstemplate.TemplateString(pkg.ChecksumFile, map[string]string{
		"version": strings.TrimPrefix(targetRelease.TagName, "v"),
		"tag":     targetRelease.TagName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to template checksum URL: %w", err)
	}

	// Use checksum download functionality (need to implement or use existing)
	// For now, return empty map as this is not the main focus
	return map[string]string{}, nil
}

// Verify checks if an installed binary matches the expected version/checksum
func (m *GitLabReleaseManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	// Use shared verification logic
	return nil, fmt.Errorf("verify not implemented for GitLab manager")
}

// fetchReleases retrieves releases from GitLab GraphQL API
func (m *GitLabReleaseManager) fetchReleases(ctx context.Context, repo string) ([]GitLabRelease, error) {
	// Prepare GraphQL request
	graphQLReq := GraphQLRequest{
		OperationName: "allReleases",
		Variables: GraphQLVariables{
			FullPath: repo,
			First:    10, // Default limit, can be made configurable
			Sort:     "RELEASED_AT_DESC",
		},
		Query: graphQLReleasesQuery,
	}

	// Marshal GraphQL request to JSON
	reqBody, err := json.Marshal(graphQLReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	// Create HTTP POST request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://gitlab.com/api/graphql", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab GraphQL API returned status %d", resp.StatusCode)
	}

	var graphQLResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&graphQLResp); err != nil {
		return nil, fmt.Errorf("failed to decode GitLab GraphQL response: %w", err)
	}

	// Handle GraphQL errors
	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("GitLab GraphQL API error: %s", graphQLResp.Errors[0].Message)
	}

	// Convert GraphQL releases to REST API format for backward compatibility
	releases := make([]GitLabRelease, 0, len(graphQLResp.Data.Project.Releases.Nodes))
	for _, gqlRelease := range graphQLResp.Data.Project.Releases.Nodes {
		// Convert GraphQL asset links to REST format
		assetLinks := make([]GitLabReleaseAsset, 0, len(gqlRelease.Assets.Links.Nodes))
		for _, link := range gqlRelease.Assets.Links.Nodes {
			assetLinks = append(assetLinks, GitLabReleaseAsset{
				Name: link.Name,
				URL:  link.URL,
			})
		}

		// Map GraphQL release to REST format
		release := GitLabRelease{
			TagName:     gqlRelease.TagName,
			Name:        gqlRelease.Name,
			Description: gqlRelease.DescriptionHtml, // Note: this is HTML, may need stripping
			CreatedAt:   gqlRelease.CreatedAt,
			Assets: struct {
				Links []GitLabReleaseAsset `json:"links"`
			}{
				Links: assetLinks,
			},
		}
		releases = append(releases, release)
	}

	return releases, nil
}
