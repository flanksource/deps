package url

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/extract"
	depshttp "github.com/flanksource/deps/pkg/http"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	depstemplate "github.com/flanksource/deps/pkg/template"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
	"github.com/flanksource/gomplate/v3"
)

// versionMetadata stores additional metadata for a version
type versionMetadata struct {
	URL      string
	Checksum string
	Asset    string
}

// URLManager implements the PackageManager interface for packages with version URLs
type URLManager struct {
	client          *http.Client
	versionMetadata map[string]map[string]*versionMetadata // pkg.Name -> version -> metadata
}

// NewURLManager creates a new URL manager
func NewURLManager() *URLManager {
	return &URLManager{
		client:          depshttp.GetHttpClient(),
		versionMetadata: make(map[string]map[string]*versionMetadata),
	}
}

// Name returns the manager identifier
func (m *URLManager) Name() string {
	return "url"
}

// DiscoverVersions fetches versions from a URL endpoint
func (m *URLManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	log := logger.GetLogger()

	if pkg.VersionsURL == "" {
		return nil, fmt.Errorf("versions_url is required for url manager")
	}

	log.Tracef("Fetching versions from: %s", pkg.VersionsURL)

	req, err := http.NewRequestWithContext(ctx, "GET", pkg.VersionsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch versions from %s: %w", pkg.VersionsURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch versions: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse JSON response
	var rawVersions interface{}
	if err := json.Unmarshal(body, &rawVersions); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Check if we should use versions_expr for structured version data
	var versions []types.Version
	if pkg.VersionsExpr != "" {
		log.Tracef("Using versions_expr to extract structured version data")
		versions, err = m.parseVersionsWithExpr(rawVersions, pkg, plat)
		if err != nil {
			return nil, fmt.Errorf("failed to parse versions with versions_expr: %w", err)
		}
	} else {
		// Create versions from JSON data using standard parsing
		versions, err = m.parseVersions(rawVersions, pkg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse versions: %w", err)
		}
	}

	// Apply version expression filtering if specified
	if pkg.VersionExpr != "" {
		filteredVersions, err := version.ApplyVersionExpr(versions, pkg.VersionExpr)
		if err != nil {
			return nil, fmt.Errorf("failed to apply version_expr: %w", err)
		}
		versions = filteredVersions
	}

	// Filter out versions that are not valid semantic versions after transformation
	versions = version.FilterToValidSemver(versions)

	log.V(2).Infof("Discovered %d versions from %s", len(versions), pkg.VersionsURL)

	// Sort versions in descending order (newest first)
	version.SortVersions(versions)

	// Apply limit if specified
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}

	return versions, nil
}

// parseVersions converts raw JSON data to Version structs
func (m *URLManager) parseVersions(data interface{}, pkg types.Package) ([]types.Version, error) {
	var versions []types.Version

	switch v := data.(type) {
	case []interface{}:
		// Handle array of versions
		for _, item := range v {
			switch versionData := item.(type) {
			case string:
				// Simple string version
				versions = append(versions, types.ParseVersion(version.Normalize(versionData), versionData))
			case map[string]interface{}:
				// Object with version fields
				ver := m.parseVersionObject(versionData)
				if ver.Version != "" {
					versions = append(versions, ver)
				}
			}
		}
	case map[string]interface{}:
		// Handle object response
		if versionsArray, ok := v["versions"].([]interface{}); ok {
			for _, item := range versionsArray {
				switch versionData := item.(type) {
				case string:
					versions = append(versions, types.ParseVersion(version.Normalize(versionData), versionData))
				case map[string]interface{}:
					ver := m.parseVersionObject(versionData)
					if ver.Version != "" {
						versions = append(versions, ver)
					}
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported JSON structure: %T", data)
	}

	return versions, nil
}

// parseVersionObject extracts version information from a version object
func (m *URLManager) parseVersionObject(obj map[string]interface{}) types.Version {
	ver := types.Version{}

	// Try to find version field
	if v, ok := obj["version"].(string); ok {
		ver.Version = version.Normalize(v)
		ver.Tag = v
	}

	// Try to find tag field
	if t, ok := obj["tag"].(string); ok {
		ver.Tag = t
		if ver.Version == "" {
			ver.Version = version.Normalize(t)
		}
	}

	// Check for prerelease flag
	if p, ok := obj["prerelease"].(bool); ok {
		ver.Prerelease = p
	} else if ver.Tag != "" {
		ver.Prerelease = isPrerelease(ver.Tag)
	}

	return ver
}

// parseVersionsWithExpr uses versions_expr to extract structured version data
func (m *URLManager) parseVersionsWithExpr(data interface{}, pkg types.Package, plat platform.Platform) ([]types.Version, error) {
	// Prepare variables for CEL expression
	vars := map[string]interface{}{
		"json": data,
		"os":   plat.OS,
		"arch": plat.Arch,
	}

	// Evaluate the versions_expr using gomplate
	resultStr, err := gomplate.RunTemplate(vars, gomplate.Template{Expression: pkg.VersionsExpr})
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate versions_expr: %w", err)
	}

	// Try to parse as JSON array
	// Remove any wrapping quotes from gomplate
	resultStr = strings.Trim(resultStr, `"'`)

	// Try to convert the result to valid JSON
	// CEL returns objects without quoted keys or string values, so we need to add them
	// This is a workaround - FIXME: find a better way to get structured data from CEL

	// Step 1: Add quotes around keys (e.g., {version: → {"version":)
	// Match pattern: word characters after { or , followed by colon and space
	reKey := regexp.MustCompile(`([{,]\s*)(\w+):\s`)
	resultStr = reKey.ReplaceAllString(resultStr, `$1"$2": `)

	// Step 2: Add quotes around string values (including version numbers)
	// Match pattern: ": value" where value is alphanumeric with special chars like :,/,.,+,-
	// This matches string values that come after a colon and are not already quoted
	reValue := regexp.MustCompile(`:\s*([a-zA-Z0-9][\w:/.+-]*)([,}\s])`)
	resultStr = reValue.ReplaceAllString(resultStr, `: "$1"$2`)

	var versionObjects []map[string]interface{}
	if err := json.Unmarshal([]byte(resultStr), &versionObjects); err != nil {
		return nil, fmt.Errorf("versions_expr must return an array of objects (got: %s): %w", resultStr[:min(100, len(resultStr))], err)
	}

	// Initialize metadata map for this package
	if m.versionMetadata[pkg.Name] == nil {
		m.versionMetadata[pkg.Name] = make(map[string]*versionMetadata)
	}

	// Extract versions and metadata
	var versions []types.Version
	for _, obj := range versionObjects {
		// Extract version (required)
		versionStr, ok := obj["version"].(string)
		if !ok || versionStr == "" {
			continue
		}

		ver := types.Version{
			Version:    version.Normalize(versionStr),
			Tag:        versionStr,
			Prerelease: isPrerelease(versionStr),
		}

		// Extract optional metadata
		metadata := &versionMetadata{}
		if url, ok := obj["url"].(string); ok {
			metadata.URL = url
		}
		if checksum, ok := obj["checksum"].(string); ok {
			metadata.Checksum = checksum
		}
		if asset, ok := obj["asset"].(string); ok {
			metadata.Asset = asset
		}

		// Store metadata if any fields are present
		if metadata.URL != "" || metadata.Checksum != "" || metadata.Asset != "" {
			m.versionMetadata[pkg.Name][ver.Version] = metadata
		}

		versions = append(versions, ver)
	}

	return versions, nil
}

// Resolve gets the download URL for a specific version and platform
func (m *URLManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	log := logger.GetLogger()

	log.Tracef("URL resolve: package=%s, version=%s, platform=%s", pkg.Name, version, plat.String())

	// Check if we have cached metadata from versions_expr
	var downloadURL, checksum, asset string
	if pkgMetadata, ok := m.versionMetadata[pkg.Name]; ok {
		if metadata, ok := pkgMetadata[version]; ok {
			log.Tracef("Using cached metadata from versions_expr")
			downloadURL = metadata.URL
			checksum = metadata.Checksum
			asset = metadata.Asset
		}
	}

	// If no cached URL, use url_template
	if downloadURL == "" {
		if pkg.URLTemplate == "" {
			return nil, fmt.Errorf("url_template is required for url manager when versions_expr doesn't provide URL")
		}

		// Resolve asset pattern if specified
		if asset == "" && len(pkg.AssetPatterns) > 0 {
		assetPattern, err := manager.ResolveAssetPattern(pkg.AssetPatterns, plat)
		if err != nil {
			return nil, err
		}

			// Template the asset pattern
			asset, err = depstemplate.TemplateURL(assetPattern, version, plat.OS, plat.Arch)
			if err != nil {
				return nil, fmt.Errorf("failed to template asset pattern: %w", err)
			}
		}

		// Template the URL with asset variable
		var err error
		downloadURL, err = m.templateURLWithAsset(pkg.URLTemplate, version, plat, asset)
		if err != nil {
			return nil, fmt.Errorf("failed to template URL: %w", err)
		}
	}

	resolution := &types.Resolution{
		Package:     pkg,
		Version:     version,
		Platform:    plat,
		DownloadURL: downloadURL,
		IsArchive:   extract.IsArchive(downloadURL),
	}

	// If we have a cached checksum, use it directly
	if checksum != "" {
		resolution.Checksum = checksum
	} else if pkg.ChecksumFile != "" {
		// Template checksum URL if available
		var checksumURL string
		var err error

		if strings.HasPrefix(pkg.ChecksumFile, "http://") || strings.HasPrefix(pkg.ChecksumFile, "https://") {
			checksumURL, err = m.templateURLWithAsset(pkg.ChecksumFile, version, plat, asset)
		} else {
			baseURL := downloadURL[:strings.LastIndex(downloadURL, "/")+1]
			checksumPath, templateErr := m.templateURLWithAsset(pkg.ChecksumFile, version, plat, asset)
			if templateErr != nil {
				err = templateErr
			} else {
				checksumURL = baseURL + checksumPath
			}
		}

		if err == nil && checksumURL != "" {
			resolution.ChecksumURL = checksumURL
		}
	}

	return resolution, nil
}

// Install downloads and installs the binary
func (m *URLManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	return fmt.Errorf("install method not implemented - use existing pipeline")
}

// GetChecksums is not supported for URL manager
func (m *URLManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	return nil, fmt.Errorf("checksums not supported for url manager")
}

// Verify checks if an installed binary matches expectations
func (m *URLManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	return nil, fmt.Errorf("verify not implemented yet")
}

// templateURLWithAsset templates a URL with version, platform, and asset variables
func (m *URLManager) templateURLWithAsset(urlTemplate, version string, plat platform.Platform, asset string) (string, error) {
	return depstemplate.TemplateURLWithAsset(urlTemplate, version, plat.OS, plat.Arch, asset)
}

// isPrerelease checks if a version string indicates a prerelease
func isPrerelease(ver string) bool {
	lower := strings.ToLower(ver)
	return strings.Contains(lower, "alpha") ||
		strings.Contains(lower, "beta") ||
		strings.Contains(lower, "rc") ||
		strings.Contains(lower, "snapshot") ||
		strings.Contains(lower, "dev")
}
