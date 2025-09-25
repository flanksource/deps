package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
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

// NewGitLabReleaseManager creates a new GitLab release manager
func NewGitLabReleaseManager(token, tokenSource string) *GitLabReleaseManager {
	return &GitLabReleaseManager{
		client:      &http.Client{},
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
		return nil, &manager.ErrPlatformNotSupported{Package: pkg.Name, Platform: plat.String()}
	}

	// Template the asset name
	assetName, err := depstemplate.TemplateString(pattern, map[string]string{
		"version": strings.TrimPrefix(targetRelease.TagName, "v"),
		"tag":     targetRelease.TagName,
		"os":      plat.OS,
		"arch":    plat.Arch,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to template asset pattern: %w", err)
	}

	// Construct download URL - GitLab uses different URL structure
	repoPath := url.PathEscape(pkg.Repo)
	downloadURL := fmt.Sprintf("https://gitlab.com/%s/-/releases/%s/downloads/%s", repoPath, targetRelease.TagName, assetName)

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

// fetchReleases retrieves releases from GitLab API
func (m *GitLabReleaseManager) fetchReleases(ctx context.Context, repo string) ([]GitLabRelease, error) {
	repoPath := url.PathEscape(repo)
	apiURL := fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/releases", repoPath)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	if m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab API returned status %d", resp.StatusCode)
	}

	var releases []GitLabRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode GitLab releases: %w", err)
	}

	return releases, nil
}