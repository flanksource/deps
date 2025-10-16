package github

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/checksum"
	"github.com/flanksource/deps/pkg/platform"
	depstemplate "github.com/flanksource/deps/pkg/template"
	"github.com/flanksource/deps/pkg/types"
	"github.com/google/go-github/v57/github"
)

// GitHubBuildManager implements the PackageManager interface for GitHub releases
// that contain multiple software versions in a single build release.
// Example: python-build-standalone where each release (tagged by build date)
// contains multiple Python versions as separate assets.
type GitHubBuildManager struct {
	// Uses shared singleton GitHub client
}

// NewGitHubBuildManager creates a new GitHub build manager.
func NewGitHubBuildManager() *GitHubBuildManager {
	return &GitHubBuildManager{}
}

// Name returns the manager identifier
func (m *GitHubBuildManager) Name() string {
	return "github_build"
}

// assetVersion represents a parsed asset with embedded version information
type assetVersion struct {
	assetName   string // Full asset name
	softwareVer string // Embedded software version (e.g., "3.11.14")
	buildDate   string // Build date from asset (e.g., "20251010")
	platformStr string // Platform string from asset (e.g., "aarch64-apple-darwin")
	downloadURL string // Download URL for the asset
}

// parseVersion splits version string into build tag and software version
// Examples:
//
//	"3.11" -> ("latest", "3.11")
//	"3.11.14" -> ("latest", "3.11.14")
//	"20251010-3.11" -> ("20251010", "3.11")
//	"20251010-3.11.14" -> ("20251010", "3.11.14")
//	"latest" -> ("latest", "latest") - will resolve to highest version in latest build
func parseVersion(version string) (buildTag string, softwareVersion string) {
	// Handle "latest" keyword
	if version == "latest" {
		return "latest", "latest"
	}

	// Check if version contains build date prefix (YYYYMMDD-)
	parts := strings.SplitN(version, "-", 2)
	if len(parts) == 2 && len(parts[0]) == 8 && isNumeric(parts[0]) {
		// Format: "20251010-3.11"
		return parts[0], parts[1]
	}

	// If version is just 8 digits (build date only), treat as build date with latest software version
	if len(version) == 8 && isNumeric(version) {
		// Format: "20251010" - use this build date with latest software version
		return version, "latest"
	}

	// Format: "3.11" - use latest build
	return "latest", version
}

// isNumeric checks if string contains only digits
func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parseAssetName extracts software version and build date from asset filename
// Example: "cpython-3.11.14+20251010-aarch64-apple-darwin-install_only.tar.gz"
// Returns: version="3.11.14", buildDate="20251010", platform="aarch64-apple-darwin"
func parseAssetName(assetName string) (version, buildDate, platformStr string, err error) {
	// Pattern: {prefix}-{version}+{builddate}-{platform}-{suffix}.{ext}
	// Platform can be 3 or 4 parts separated by dashes:
	//   - aarch64-apple-darwin (3 parts)
	//   - x86_64-unknown-linux-gnu (4 parts)
	re := regexp.MustCompile(`^[^-]+-([0-9]+\.[0-9]+\.[0-9]+)\+([0-9]{8})-([^-]+-[^-]+-[^-]+(?:-[^-]+)?)-`)
	matches := re.FindStringSubmatch(assetName)
	if len(matches) < 4 {
		return "", "", "", fmt.Errorf("failed to parse asset name: %s", assetName)
	}
	return matches[1], matches[2], matches[3], nil
}

// DiscoverVersions returns available software versions from the latest build
func (m *GitHubBuildManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	if pkg.Repo == "" {
		return nil, fmt.Errorf("repo is required for GitHub build manager")
	}

	parts := strings.Split(pkg.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s (expected owner/repo)", pkg.Repo)
	}
	owner, repo := parts[0], parts[1]

	// Always use "latest" release for version discovery
	client := GetClient().Client()
	logger.V(3).Infof("GitHub Build: Fetching 'latest' release from %s/%s", owner, repo)

	release, _, err := client.Repositories.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest release for %s: %w", pkg.Repo, err)
	}

	logger.V(4).Infof("GitHub Build: Found release %s with %d assets", release.GetTagName(), len(release.Assets))

	// Parse all assets to extract unique software versions
	assetVersions, err := m.parseAssetsForPlatform(release.Assets, plat)
	if err != nil {
		return nil, err
	}

	// Convert to types.Version and deduplicate
	versionMap := make(map[string]types.Version)
	for _, av := range assetVersions {
		if _, exists := versionMap[av.softwareVer]; !exists {
			versionMap[av.softwareVer] = types.Version{
				Version:   av.softwareVer,
				Tag:       av.buildDate, // Store build date as tag
				Published: release.GetPublishedAt().Time,
			}
		}
	}

	// Convert map to slice
	var versions []types.Version
	for _, v := range versionMap {
		versions = append(versions, v)
	}

	// Sort by semantic version (descending)
	sort.Slice(versions, func(i, j int) bool {
		v1, err1 := semver.NewVersion(versions[i].Version)
		v2, err2 := semver.NewVersion(versions[j].Version)
		if err1 != nil || err2 != nil {
			return versions[i].Version > versions[j].Version
		}
		return v1.GreaterThan(v2)
	})

	// Apply limit
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}

	logger.V(3).Infof("GitHub Build: Found %d unique software versions in latest build", len(versions))
	return versions, nil
}

// parseAssetsForPlatform extracts software versions from assets matching the platform
func (m *GitHubBuildManager) parseAssetsForPlatform(assets []*github.ReleaseAsset, plat platform.Platform) ([]assetVersion, error) {
	// Map platform to expected string in asset name
	platformMap := map[string]string{
		"darwin-amd64":  "x86_64-apple-darwin",
		"darwin-arm64":  "aarch64-apple-darwin",
		"linux-amd64":   "x86_64-unknown-linux-gnu",
		"linux-arm64":   "aarch64-unknown-linux-gnu",
		"windows-amd64": "x86_64-pc-windows-msvc",
	}

	expectedPlatform := platformMap[plat.String()]
	if expectedPlatform == "" {
		return nil, fmt.Errorf("unsupported platform: %s", plat.String())
	}

	var result []assetVersion
	for _, asset := range assets {
		if asset.Name == nil {
			continue
		}

		assetName := *asset.Name

		// Skip non-install_only assets
		if !strings.Contains(assetName, "-install_only.tar.gz") {
			continue
		}

		// Parse asset name
		version, buildDate, platformStr, err := parseAssetName(assetName)
		if err != nil {
			logger.V(5).Infof("Skipping asset %s: %v", assetName, err)
			continue
		}

		// Filter by platform
		if platformStr != expectedPlatform {
			continue
		}

		result = append(result, assetVersion{
			assetName:   assetName,
			softwareVer: version,
			buildDate:   buildDate,
			platformStr: platformStr,
			downloadURL: asset.GetBrowserDownloadURL(),
		})
	}

	return result, nil
}

// Resolve gets the download URL and metadata for a specific version and platform
func (m *GitHubBuildManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	parts := strings.Split(pkg.Repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s", pkg.Repo)
	}
	owner, repo := parts[0], parts[1]

	// Parse version to extract build tag and software version
	buildTag, softwareVersion := parseVersion(version)
	logger.V(3).Infof("GitHub Build: Resolving %s@%s (build=%s, software=%s, platform=%s)",
		pkg.Name, version, buildTag, softwareVersion, plat.String())

	var release *github.RepositoryRelease
	var err error

	client := GetClient().Client()
	if buildTag == "latest" {
		release, _, err = client.Repositories.GetLatestRelease(ctx, owner, repo)
	} else {
		release, _, err = client.Repositories.GetReleaseByTag(ctx, owner, repo, buildTag)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get release %s for %s: %w", buildTag, pkg.Repo, err)
	}

	logger.V(4).Infof("GitHub Build: Fetched release %s with %d assets", release.GetTagName(), len(release.Assets))

	// Parse all assets for this platform
	assetVersions, err := m.parseAssetsForPlatform(release.Assets, plat)
	if err != nil {
		return nil, err
	}

	if len(assetVersions) == 0 {
		return nil, fmt.Errorf("no assets found for platform %s in release %s", plat.String(), release.GetTagName())
	}

	// Find matching software version
	var matched *assetVersion
	if softwareVersion == "latest" {
		// Pick the highest version available
		if len(assetVersions) == 0 {
			return nil, fmt.Errorf("no assets found for platform %s in release %s", plat.String(), release.GetTagName())
		}
		// Sort by version descending and pick first
		sort.Slice(assetVersions, func(i, j int) bool {
			v1, err1 := semver.NewVersion(assetVersions[i].softwareVer)
			v2, err2 := semver.NewVersion(assetVersions[j].softwareVer)
			if err1 != nil || err2 != nil {
				return assetVersions[i].softwareVer > assetVersions[j].softwareVer
			}
			return v1.GreaterThan(v2)
		})
		matched = &assetVersions[0]
		logger.V(3).Infof("GitHub Build: Selected latest version %s from release %s", matched.softwareVer, release.GetTagName())
	} else {
		var err error
		matched, err = m.findMatchingVersion(assetVersions, softwareVersion)
		if err != nil {
			return nil, err
		}
	}

	logger.V(3).Infof("GitHub Build: Matched asset %s (version=%s, build=%s)",
		matched.assetName, matched.softwareVer, matched.buildDate)

	// Build resolution
	resolution := &types.Resolution{
		Package:     pkg,
		Version:     matched.softwareVer,
		Platform:    plat,
		DownloadURL: matched.downloadURL,
		IsArchive:   true,
		GitHubAsset: &types.GitHubAsset{
			Repo:        pkg.Repo,
			Tag:         release.GetTagName(),
			AssetName:   matched.assetName,
			DownloadURL: matched.downloadURL,
		},
	}

	// Guess binary path
	resolution.BinaryPath = m.guessBinaryPath(pkg, matched.assetName, plat)

	return resolution, nil
}

// ResolveVersionConstraint resolves a version constraint to a concrete software version
// Handles build tags (YYYYMMDD) by resolving them to actual software versions
func (m *GitHubBuildManager) ResolveVersionConstraint(ctx context.Context, pkg types.Package, constraint string, plat platform.Platform) (string, error) {
	// Parse the constraint to understand what type it is
	buildTag, softwareVersion := parseVersion(constraint)

	logger.V(3).Infof("GitHub Build: Resolving constraint %s (buildTag=%s, softwareVersion=%s)",
		constraint, buildTag, softwareVersion)

	// If constraint is just a build tag (no software version specified),
	// we need to resolve it to get the actual software version
	if softwareVersion == "latest" && buildTag != "latest" {
		// Constraint is just a build tag like "20251010"
		// Need to resolve via Resolve() to get the actual software version
		logger.V(3).Infof("GitHub Build: Build tag %s detected, resolving to software version", buildTag)

		resolution, err := m.Resolve(ctx, pkg, constraint, plat)
		if err != nil {
			return "", fmt.Errorf("failed to resolve build tag %s: %w", buildTag, err)
		}

		logger.V(3).Infof("GitHub Build: Build tag %s resolved to software version %s", buildTag, resolution.Version)
		return resolution.Version, nil
	}

	// If constraint contains a software version (like "3.11" or "20251010-3.11.14"),
	// use standard version discovery
	logger.V(3).Infof("GitHub Build: Using standard resolution for software version %s", softwareVersion)

	// Use the software version part for standard resolution
	versions, err := m.DiscoverVersions(ctx, pkg, plat, 0)
	if err != nil {
		return "", fmt.Errorf("failed to discover versions: %w", err)
	}

	// Find matching version
	if softwareVersion == "latest" {
		// Return latest software version
		if len(versions) > 0 {
			return versions[0].Version, nil
		}
		return "", fmt.Errorf("no versions available")
	}

	// Try exact match first
	for _, v := range versions {
		if v.Version == softwareVersion {
			return v.Version, nil
		}
	}

	// Try constraint matching (for "3.11" style constraints)
	semverConstraint, err := semver.NewConstraint("~" + softwareVersion)
	if err == nil {
		// Sort by version descending
		sort.Slice(versions, func(i, j int) bool {
			v1, err1 := semver.NewVersion(versions[i].Version)
			v2, err2 := semver.NewVersion(versions[j].Version)
			if err1 != nil || err2 != nil {
				return versions[i].Version > versions[j].Version
			}
			return v1.GreaterThan(v2)
		})

		// Find first matching version
		for _, v := range versions {
			ver, err := semver.NewVersion(v.Version)
			if err == nil && semverConstraint.Check(ver) {
				logger.V(3).Infof("GitHub Build: Constraint ~%s matched version %s", softwareVersion, v.Version)
				return v.Version, nil
			}
		}
	}

	return "", fmt.Errorf("version %s not found", softwareVersion)
}

// findMatchingVersion finds the best matching version from available assets
// Supports exact match (3.11.14) or constraint match (~3.11 for highest 3.11.x)
func (m *GitHubBuildManager) findMatchingVersion(assets []assetVersion, targetVersion string) (*assetVersion, error) {
	if len(assets) == 0 {
		return nil, fmt.Errorf("no assets available")
	}

	// Try exact match first
	for _, asset := range assets {
		if asset.softwareVer == targetVersion {
			return &asset, nil
		}
	}

	// Try constraint match (e.g., "3.11" matches highest "3.11.x")
	constraint, err := semver.NewConstraint("~" + targetVersion)
	if err == nil {
		// Sort by version descending
		sort.Slice(assets, func(i, j int) bool {
			v1, err1 := semver.NewVersion(assets[i].softwareVer)
			v2, err2 := semver.NewVersion(assets[j].softwareVer)
			if err1 != nil || err2 != nil {
				return assets[i].softwareVer > assets[j].softwareVer
			}
			return v1.GreaterThan(v2)
		})

		// Find first version matching constraint
		for _, asset := range assets {
			v, err := semver.NewVersion(asset.softwareVer)
			if err == nil && constraint.Check(v) {
				logger.V(4).Infof("GitHub Build: Version %s matches constraint ~%s", asset.softwareVer, targetVersion)
				return &asset, nil
			}
		}
	}

	// No match found
	availableVersions := make([]string, len(assets))
	for i, asset := range assets {
		availableVersions[i] = asset.softwareVer
	}
	return nil, fmt.Errorf("version %s not found. Available versions: %s",
		targetVersion, strings.Join(availableVersions, ", "))
}

// Install downloads and installs the binary
func (m *GitHubBuildManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	return fmt.Errorf("install method not yet implemented - use existing Install")
}

// GetChecksums retrieves checksums for all platforms
func (m *GitHubBuildManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	// GitHub Build releases typically don't have checksum files
	// Individual assets would need to be checksummed after download
	return nil, fmt.Errorf("checksums not available for github_build manager")
}

// Verify checks if an installed binary matches expectations
func (m *GitHubBuildManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	return nil, fmt.Errorf("verify not implemented yet")
}

// WhoAmI returns authentication status and user information for GitHub
func (m *GitHubBuildManager) WhoAmI(ctx context.Context) *types.AuthStatus {
	// Reuse the same WhoAmI from GitHubReleaseManager
	releaseManager := NewGitHubReleaseManager()
	return releaseManager.WhoAmI(ctx)
}

// Helper methods

func (m *GitHubBuildManager) guessBinaryPath(pkg types.Package, assetName string, plat platform.Platform) string {
	// First check if BinaryPath is specified (supports CEL expressions)
	if pkg.BinaryPath != "" {
		data := map[string]interface{}{
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

	// Fall back to BinaryName if specified
	if pkg.BinaryName != "" {
		return pkg.BinaryName
	}

	// Default pattern for binaries
	baseName := pkg.Name
	if plat.IsWindows() {
		baseName += ".exe"
	}
	return baseName
}

func (m *GitHubBuildManager) downloadAndParseChecksums(ctx context.Context, url string) (map[string]string, error) {
	discovery := checksum.NewDiscovery()
	resolution := &types.Resolution{
		ChecksumURL: url,
	}
	return discovery.FindChecksums(ctx, resolution)
}
