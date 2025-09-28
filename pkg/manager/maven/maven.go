package maven

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
)

// MavenManager implements the PackageManager interface for Maven repositories
type MavenManager struct {
	client *http.Client
}

// MavenMetadata represents the maven-metadata.xml structure
type MavenMetadata struct {
	XMLName    xml.Name `xml:"metadata"`
	GroupID    string   `xml:"groupId"`
	ArtifactID string   `xml:"artifactId"`
	Versioning struct {
		Latest      string `xml:"latest"`
		Release     string `xml:"release"`
		LastUpdated string `xml:"lastUpdated"`
		Versions    struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

// MavenCoordinates represents a parsed Maven coordinate
type MavenCoordinates struct {
	GroupID    string
	ArtifactID string
	Version    string
	Packaging  string
	Classifier string
}

// NewMavenManager creates a new Maven manager
func NewMavenManager() *MavenManager {
	return &MavenManager{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the manager identifier
func (m *MavenManager) Name() string {
	return "maven"
}

// DiscoverVersions returns available versions from Maven metadata matching the constraint
func (m *MavenManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	coords, err := m.parseCoordinates(pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Maven coordinates: %w", err)
	}

	// Template the coordinates using platform info for metadata fetching
	coords, err = m.templateCoordinates(coords, "latest", plat)
	if err != nil {
		return nil, fmt.Errorf("failed to template coordinates: %w", err)
	}

	repository := m.getRepository(pkg)
	metadataURL := m.buildMetadataURL(repository, coords)

	// Debug: Maven fetching metadata from: %s

	metadata, err := m.fetchMetadata(ctx, metadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Maven metadata: %w", err)
	}

	var versions []types.Version
	for _, ver := range metadata.Versioning.Versions.Version {
		versions = append(versions, types.Version{
			Version:    version.Normalize(ver),
			Tag:        ver,
			Prerelease: strings.Contains(strings.ToLower(ver), "snapshot"),
		})
	}

	// Debug: Maven found %d versions in metadata

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
func (m *MavenManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	coords, err := m.parseCoordinates(pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Maven coordinates: %w", err)
	}

	// Debug: Maven resolve: package=%s, version=%s, platform=%s

	// Template the coordinates if they contain platform variables
	coords, err = m.templateCoordinates(coords, version, plat)
	if err != nil {
		return nil, fmt.Errorf("failed to template coordinates: %w", err)
	}

	coords.Version = version
	repository := m.getRepository(pkg)

	downloadURL := m.buildArtifactURL(repository, coords)
	checksumURL := downloadURL + ".sha256"

	// Debug: Maven built URLs: download=%s, checksum=%s

	// Verify the artifact exists
	if err := m.verifyArtifactExists(ctx, downloadURL); err != nil {
		// Return enhanced error with version suggestions
		return nil, m.enhanceErrorWithVersions(ctx, pkg, coords, err)
	}

	resolution := &types.Resolution{
		Package:     pkg,
		Version:     version,
		Platform:    plat,
		DownloadURL: downloadURL,
		ChecksumURL: checksumURL,
		IsArchive:   coords.Packaging == "jar" || coords.Packaging == "war" || coords.Packaging == "zip",
	}

	return resolution, nil
}

// Install downloads and installs the artifact
func (m *MavenManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	// For now, return not implemented - the actual installation
	// is handled by the existing deps.Install function
	return fmt.Errorf("install method not yet implemented - use existing Install")
}

// GetChecksums retrieves checksums for all platforms
func (m *MavenManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	coords, err := m.parseCoordinates(pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Maven coordinates: %w", err)
	}

	coords.Version = version
	repository := m.getRepository(pkg)

	checksumURL := m.buildArtifactURL(repository, coords) + ".sha256"

	resp, err := m.client.Get(checksumURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download checksum: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksum file not found: %s", checksumURL)
	}

	checksumBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read checksum: %w", err)
	}

	checksum := strings.TrimSpace(string(checksumBytes))

	// Return a map with the artifact filename as key
	artifactName := fmt.Sprintf("%s-%s", coords.ArtifactID, coords.Version)
	if coords.Classifier != "" {
		artifactName += "-" + coords.Classifier
	}
	artifactName += "." + coords.Packaging

	return map[string]string{
		artifactName: checksum,
	}, nil
}

// Verify checks if an installed artifact matches expectations
func (m *MavenManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	return nil, fmt.Errorf("verify not implemented yet")
}

// Helper methods

func (m *MavenManager) parseCoordinates(pkg types.Package) (*MavenCoordinates, error) {
	coords := &MavenCoordinates{
		Packaging: "jar", // Default packaging
	}

	if pkg.Extra == nil {
		return nil, fmt.Errorf("Maven package requires 'extra' configuration")
	}

	extra := pkg.Extra

	if groupID, exists := extra["group_id"]; exists {
		coords.GroupID = fmt.Sprintf("%v", groupID)
	} else {
		return nil, fmt.Errorf("group_id is required in extra configuration")
	}

	if artifactID, exists := extra["artifact_id"]; exists {
		coords.ArtifactID = fmt.Sprintf("%v", artifactID)
	} else {
		return nil, fmt.Errorf("artifact_id is required in extra configuration")
	}

	if packaging, exists := extra["packaging"]; exists {
		coords.Packaging = fmt.Sprintf("%v", packaging)
	}

	if classifier, exists := extra["classifier"]; exists {
		coords.Classifier = fmt.Sprintf("%v", classifier)
	}

	return coords, nil
}

func (m *MavenManager) templateCoordinates(coords *MavenCoordinates, version string, plat platform.Platform) (*MavenCoordinates, error) {
	data := map[string]string{
		"version": version,
		"os":      plat.OS,
		"arch":    plat.Arch,
	}

	// Template artifact ID
	if strings.Contains(coords.ArtifactID, "{{") {
		templated, err := m.templateString(coords.ArtifactID, data)
		if err != nil {
			return nil, fmt.Errorf("failed to template artifact_id: %w", err)
		}
		coords.ArtifactID = templated
	}

	// Template classifier if present
	if coords.Classifier != "" && strings.Contains(coords.Classifier, "{{") {
		templated, err := m.templateString(coords.Classifier, data)
		if err != nil {
			return nil, fmt.Errorf("failed to template classifier: %w", err)
		}
		coords.Classifier = templated
	}

	return coords, nil
}

func (m *MavenManager) templateString(pattern string, data map[string]string) (string, error) {
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

func (m *MavenManager) getRepository(pkg types.Package) string {
	if pkg.Extra != nil {
		if repo, exists := pkg.Extra["repository"]; exists {
			return fmt.Sprintf("%v", repo)
		}
	}
	return "https://repo1.maven.org/maven2" // Default to Maven Central
}

func (m *MavenManager) buildMetadataURL(repository string, coords *MavenCoordinates) string {
	groupPath := strings.ReplaceAll(coords.GroupID, ".", "/")
	return fmt.Sprintf("%s/%s/%s/maven-metadata.xml", repository, groupPath, coords.ArtifactID)
}

func (m *MavenManager) buildArtifactURL(repository string, coords *MavenCoordinates) string {
	groupPath := strings.ReplaceAll(coords.GroupID, ".", "/")
	artifactName := fmt.Sprintf("%s-%s", coords.ArtifactID, coords.Version)

	if coords.Classifier != "" {
		artifactName += "-" + coords.Classifier
	}

	artifactName += "." + coords.Packaging

	return fmt.Sprintf("%s/%s/%s/%s/%s", repository, groupPath, coords.ArtifactID, coords.Version, artifactName)
}

func (m *MavenManager) fetchMetadata(ctx context.Context, url string) (*MavenMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch metadata from %s: HTTP %d", url, resp.StatusCode)
	}

	var metadata MavenMetadata
	decoder := xml.NewDecoder(resp.Body)
	if err := decoder.Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata XML: %w", err)
	}

	return &metadata, nil
}

func (m *MavenManager) verifyArtifactExists(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return err
	}

	// Debug: Maven checking artifact exists: %s

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Debug: Maven artifact check response: %s returned HTTP %d

	if resp.StatusCode == http.StatusNotFound {
		return &manager.ErrVersionNotFound{
			Package: url,
			Version: "requested",
		}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("artifact not accessible at %s: HTTP %d", url, resp.StatusCode)
	}

	return nil
}

func (m *MavenManager) enhanceErrorWithVersions(ctx context.Context, pkg types.Package, coords *MavenCoordinates, originalErr error) error {
	// Try to get available versions using a default platform for error enhancement
	defaultPlatform := platform.Platform{OS: "linux", Arch: "amd64"}
	versions, err := m.DiscoverVersions(ctx, pkg, defaultPlatform, 20)
	if err != nil {
		// If we can't get versions, return the original error
		return originalErr
	}

	packageName := fmt.Sprintf("%s:%s", coords.GroupID, coords.ArtifactID)
	return manager.EnhanceErrorWithVersions(packageName, coords.Version, versions, originalErr)
}
