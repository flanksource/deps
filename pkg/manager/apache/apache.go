package apache

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/extract"
	depshttp "github.com/flanksource/deps/pkg/http"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
	"github.com/samber/lo"
	"golang.org/x/net/html"
)

// ApacheManager implements the PackageManager interface for Apache archives
type ApacheManager struct {
	client             *http.Client
	defaultURLTemplate string
}

// NewApacheManager creates a new Apache archives manager
func NewApacheManager() *ApacheManager {
	return &ApacheManager{
		client:             depshttp.GetHttpClient(),
		defaultURLTemplate: "https://archive.apache.org/dist/{{.name}}/{{.asset}}",
	}
}

// Name returns the manager identifier
func (m *ApacheManager) Name() string {
	return "apache"
}

// DiscoverVersions returns available versions from Apache archives
func (m *ApacheManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {

	logger.Tracef("Discovering versions for package %s on platform %s: %s", pkg.Name, plat.String(), pkg.URLTemplate)

	url := pkg.URLTemplate
	if url == "" {
		url = m.defaultURLTemplate
	}
	url, err := pkg.Template(url, plat, "")
	if err != nil {
		return nil, fmt.Errorf("failed to template URL for package %s: %w", pkg.Name, err)
	}

	url, _ = filepath.Split(url) // Ignore the filename part

	// Try to discover versions from the directory listing
	versions, err := m.discoverVersionsFromListing(ctx, pkg, url)
	if err != nil {
		return nil, fmt.Errorf("failed to discover versions for %s: %w", pkg.Name, err)
	}

	// Apply version expression filtering if specified
	if pkg.VersionExpr != "" {
		filteredVersions, err := version.ApplyVersionExpr(versions, pkg.VersionExpr)
		if err != nil {
			return nil, fmt.Errorf("failed to apply version_expr for %s: %w", pkg.Name, err)
		}
		versions = filteredVersions
	}

	// Filter out versions that are not valid semantic versions after transformation
	versions = version.FilterToValidSemver(versions)

	// Sort versions in descending order (newest first)
	version.SortVersions(versions)

	// Apply limit if specified
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}

	return versions, nil
}

// Resolve gets the download URL for a specific version and platform
func (m *ApacheManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	log := logger.GetLogger()
	log.Tracef("Apache resolve: package=%s, version=%s, platform=%s", pkg.Name, version, plat.String())

	// Validate that the requested version exists
	validVersion, err := m.validateVersion(ctx, pkg, version, plat)
	if err != nil {
		// If it's a version not found error, enhance it with available versions
		if versionErr, ok := err.(*manager.ErrVersionNotFound); ok {
			return nil, m.enhanceErrorWithVersions(ctx, pkg, versionErr.Version, plat, err)
		}
		return nil, err
	}
	log.V(4).Infof("Apache resolve: validated version: %s", validVersion)

	// Use custom URL template if provided, otherwise use default
	urlTemplate := m.defaultURLTemplate
	if pkg.URLTemplate != "" {
		urlTemplate = pkg.URLTemplate
	}
	// Normalize URL template to auto-append {{.asset}} if it ends with /
	urlTemplate = manager.NormalizeURLTemplate(urlTemplate)

	// Resolve asset name from patterns using the validated version
	asset, err := m.resolveAssetPattern(pkg, validVersion, plat)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve asset: %w", err)
	}

	// Template the URL using the validated version
	downloadURL, err := m.templateURL(urlTemplate, pkg.Name, validVersion, asset, plat)
	if err != nil {
		return nil, fmt.Errorf("failed to template URL: %w", err)
	}

	// Build checksum URL if checksum file pattern is provided
	var checksumURL string
	if pkg.ChecksumFile != "" {
		checksumURL, err = m.templateString(pkg.ChecksumFile, map[string]string{
			"name":    pkg.Name,
			"version": validVersion,
			"asset":   asset,
			"os":      plat.OS,
			"arch":    plat.Arch,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to template checksum file: %w", err)
		}
		// Make checksum URL absolute if it's relative
		if !strings.HasPrefix(checksumURL, "http") {
			checksumURL = strings.Replace(downloadURL, asset, checksumURL, 1)
		}
	}

	// Default Apache packages to directory mode if not explicitly set
	if pkg.Mode == "" {
		pkg.Mode = "directory"
	}

	resolution := &types.Resolution{
		Package:     pkg,
		Version:     validVersion,
		Platform:    plat,
		DownloadURL: downloadURL,
		ChecksumURL: checksumURL,
		IsArchive:   extract.IsArchive(downloadURL),
	}

	return resolution, nil
}

// Install downloads and installs the artifact
func (m *ApacheManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	return fmt.Errorf("install method not implemented - use existing pipeline")
}

// GetChecksums retrieves checksums for all platforms
func (m *ApacheManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	if pkg.ChecksumFile == "" {
		return nil, fmt.Errorf("no checksum file pattern specified for package %s", pkg.Name)
	}

	// For simplicity, we'll return checksums for the current platform only
	// A full implementation would iterate over all supported platforms
	plat := platform.Platform{OS: "linux", Arch: "amd64"}

	resolution, err := m.Resolve(ctx, pkg, version, plat)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve package for checksum: %w", err)
	}

	if resolution.ChecksumURL == "" {
		return nil, fmt.Errorf("no checksum URL available for package %s", pkg.Name)
	}

	checksum, err := m.fetchChecksum(ctx, resolution.ChecksumURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch checksum: %w", err)
	}

	asset, _ := m.resolveAssetPattern(pkg, version, plat)
	return map[string]string{
		asset: checksum,
	}, nil
}

// Verify checks if an installed artifact matches expectations
func (m *ApacheManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	return nil, fmt.Errorf("verify not implemented yet")
}

// Helper methods

// resolveAssetPattern resolves the asset pattern for a package and platform, then templates it
func (m *ApacheManager) resolveAssetPattern(pkg types.Package, version string, plat platform.Platform) (string, error) {
	// Use common asset pattern resolution
	pattern, err := manager.ResolveAssetPattern(pkg.AssetPatterns, plat, pkg.Name)
	if err != nil {
		return "", err
	}

	// Template the pattern
	asset, err := m.templateString(pattern, map[string]string{
		"name":    pkg.Name,
		"version": version,
		"os":      plat.OS,
		"arch":    plat.Arch,
		"tag":     "v" + version, // For compatibility
	})
	if err != nil {
		return "", fmt.Errorf("failed to template asset pattern: %w", err)
	}

	return asset, nil
}

func (m *ApacheManager) templateURL(urlTemplate, name, version, asset string, plat platform.Platform) (string, error) {
	return m.templateString(urlTemplate, map[string]string{
		"name":    name,
		"version": version,
		"asset":   asset,
		"os":      plat.OS,
		"arch":    plat.Arch,
		"tag":     "v" + version,
	})
}

func (m *ApacheManager) templateString(pattern string, data map[string]string) (string, error) {
	tmpl, err := template.New("pattern").Parse(pattern)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (m *ApacheManager) discoverVersionsFromListing(ctx context.Context, pkg types.Package, baseURL string) ([]types.Version, error) {
	log := logger.GetLogger()

	// Try fetching from the base URL first
	versions, err := m.fetchVersionsFromURL(ctx, pkg, baseURL)
	if err == nil && len(versions) > 0 {
		return versions, nil
	}

	// If that fails or returns no versions, try with binaries/ subdirectory
	if !strings.HasSuffix(baseURL, "/") {
		baseURL = baseURL + "/"
	}
	binariesURL := baseURL + "binaries/"

	log.V(2).Infof("Trying binaries/ subdirectory: %s", binariesURL)
	versions, binErr := m.fetchVersionsFromURL(ctx, pkg, binariesURL)
	if binErr == nil && len(versions) > 0 {
		return versions, nil
	}

	// If both fail, return the original error
	if err != nil {
		return nil, err
	}
	return versions, nil
}

func (m *ApacheManager) fetchVersionsFromURL(ctx context.Context, pkg types.Package, url string) ([]types.Version, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch directory listing from %s: HTTP %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	versions := m.ParseVersionsFromHTML(pkg, string(body))

	return versions, nil
}

// ParseVersionsFromHTML extracts version information from Apache directory listing HTML
func (m *ApacheManager) ParseVersionsFromHTML(pkg types.Package, htmlContent string) []types.Version {
	log := logger.GetLogger()
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		log.Warnf("Failed to parse HTML: %v", err)
		return m.parseVersionsFromHTMLFallback(htmlContent)
	}

	var hrefs []string
	var findLinks func(*html.Node)
	findLinks = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					hrefs = append(hrefs, attr.Val)
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findLinks(c)
		}
	}
	findLinks(doc)

	var versions []types.Version
	versionSet := make(map[string]bool)
	versionStrings := m.extractVersionFromHref(hrefs...)

	if len(versionStrings) == 0 {
		hrefs = lo.Map(hrefs, func(href string, _ int) string {
			return strings.TrimPrefix(href, pkg.Name+"-")
		})
		versionStrings = m.extractVersionFromHref(hrefs...)
	}

	// Try to extract version from the href
	for _, ver := range versionStrings {

		if ver != "" && !versionSet[ver] {
			versionSet[ver] = true
			versions = append(versions, types.ParseVersion(version.Normalize(ver), ver))
		}
	}

	log.V(4).Infof("Parsed %d versions from HTML", len(versions))
	if len(versions) == 0 {
		log.V(2).Infof("No versions found in HTML, trying fallback parser")
		return m.parseVersionsFromHTMLFallback(htmlContent)
	}

	return versions
}

// extractVersionFromHref extracts version from various Apache href patterns
func (m *ApacheManager) extractVersionFromHref(hrefs ...string) []string {
	versions := []string{}
	for _, href := range hrefs {
		logger.V(5).Infof("Found href: %s", href)
		// Skip query parameters, parent directory, and non-relevant files
		if strings.Contains(href, "?") || href == "../" {
			continue
		}

		// Skip checksum and signature files
		if strings.HasSuffix(href, ".asc") || strings.HasSuffix(href, ".md5") ||
			strings.HasSuffix(href, ".sha1") || strings.HasSuffix(href, ".sha256") ||
			strings.HasSuffix(href, ".sha512") {
			continue
		}

		// Skip release notes and other metadata
		if strings.HasPrefix(href, "RELEASE-NOTES") {
			continue
		}

		// Pattern 1: Version directory (e.g., "3.9.0/" for maven, "v1.0.0/" variants)
		if strings.HasSuffix(href, "/") {
			dir := strings.TrimSuffix(href, "/")
			// Remove 'v' prefix if present
			dir = strings.TrimPrefix(dir, "v")
			if isVersionLike(dir) {
				versions = append(versions, dir)
			}
		}

		// Pattern 2: Filename with version (e.g., "apache-ant-1.10.15-bin.tar.gz")
		// Extract version from filename patterns like: apache-{name}-{version}-{suffix}.{ext}
		// Match version but exclude common suffixes like -bin, -src
		versionPattern := regexp.MustCompile(`-([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:\.[0-9]+)?(?:-(?:alpha|beta|rc|M|milestone)[0-9]*)?)-(?:bin|src|all|distribution)\.(?:tar\.gz|tar\.bz2|zip|tgz)`)
		if matches := versionPattern.FindStringSubmatch(href); len(matches) > 1 {
			versions = append(versions, matches[1])
		}

		// Pattern 3: Try without the suffix requirement (for other formats)
		versionPattern2 := regexp.MustCompile(`-([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:\.[0-9]+)?(?:-(?:alpha|beta|rc|M|milestone)[0-9]*)?)\.(?:tar\.gz|tar\.bz2|zip|tgz)`)
		if matches := versionPattern2.FindStringSubmatch(href); len(matches) > 1 {
			ver := matches[1]
			// Only return if it doesn't end with -bin, -src, etc.
			if !strings.HasSuffix(ver, "-bin") && !strings.HasSuffix(ver, "-src") {
				versions = append(versions, ver)
			}
		}
	}
	return versions
}

// isVersionLike checks if a string looks like a version number
func isVersionLike(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Remove leading 'v' if present
	s = strings.TrimPrefix(s, "v")
	// Must start with a digit
	if s[0] < '0' || s[0] > '9' {
		return false
	}
	// Must contain at least one dot
	return strings.Contains(s, ".")
}

// parseVersionsFromHTMLFallback uses regex as a fallback when HTML parsing fails
func (m *ApacheManager) parseVersionsFromHTMLFallback(htmlContent string) []types.Version {
	log := logger.GetLogger()
	log.V(2).Infof("Using fallback regex parser for HTML")

	versionRegexes := []*regexp.Regexp{
		regexp.MustCompile(`href="([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:\.[0-9]+)?(?:-[a-zA-Z0-9-]+)?)/?"`),
		regexp.MustCompile(`href="v([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:\.[0-9]+)?(?:-[a-zA-Z0-9-]+)?)/?"`),
		regexp.MustCompile(`href="[^"]*-([0-9]+\.[0-9]+(?:\.[0-9]+)?(?:\.[0-9]+)?(?:-[a-zA-Z0-9-]+)?)(?:-[a-z]+)?\.(?:tar\.gz|tar\.bz2|zip|tgz)"`),
	}

	var versions []types.Version
	versionSet := make(map[string]bool)

	for _, regex := range versionRegexes {
		matches := regex.FindAllStringSubmatch(htmlContent, -1)
		for _, match := range matches {
			if len(match) > 1 {
				ver := match[1]
				if !versionSet[ver] {
					versionSet[ver] = true
					versions = append(versions, types.ParseVersion(version.Normalize(ver), ver))
				}
			}
		}
	}

	return versions
}

func (m *ApacheManager) fetchChecksum(ctx context.Context, checksumURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", checksumURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum file not found: %s", checksumURL)
	}

	checksumBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read checksum: %w", err)
	}

	checksum := strings.TrimSpace(string(checksumBytes))

	// Handle different checksum file formats
	// Some files contain just the hash, others contain "hash filename"
	parts := strings.Fields(checksum)
	if len(parts) > 0 {
		return parts[0], nil
	}

	return checksum, nil
}

// validateVersion checks if the requested version exists in the available versions
func (m *ApacheManager) validateVersion(ctx context.Context, pkg types.Package, requestedVersion string, plat platform.Platform) (string, error) {
	// Get available versions to validate against
	versions, err := m.DiscoverVersions(ctx, pkg, plat, 0) // Get all versions
	if err != nil {
		return "", fmt.Errorf("failed to discover available versions: %w", err)
	}

	// Normalize the requested version for comparison
	normalizedRequested := version.Normalize(requestedVersion)

	// Check if the requested version exists in available versions
	for _, v := range versions {
		if v.Version == normalizedRequested || v.Tag == requestedVersion {
			return v.Tag, nil // Return the original tag for URL building
		}
	}

	return "", &manager.ErrVersionNotFound{
		Package: pkg.Name,
		Version: requestedVersion,
	}
}

// enhanceErrorWithVersions enhances version not found errors with available version suggestions
func (m *ApacheManager) enhanceErrorWithVersions(ctx context.Context, pkg types.Package, requestedVersion string, plat platform.Platform, originalErr error) error {
	// Try to get available versions using a default platform for error enhancement
	versions, err := m.DiscoverVersions(ctx, pkg, plat, 20)
	if err != nil {
		// If we can't get versions, return the original error
		return originalErr
	}

	return manager.EnhanceErrorWithVersions(pkg.Name, requestedVersion, versions, originalErr)
}
