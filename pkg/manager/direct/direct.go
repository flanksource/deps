package direct

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flanksource/deps/pkg/platform"
	depstemplate "github.com/flanksource/deps/pkg/template"
	"github.com/flanksource/deps/pkg/types"
)

// DirectURLManager implements the PackageManager interface for direct URL downloads
type DirectURLManager struct{}

// NewDirectURLManager creates a new direct URL manager
func NewDirectURLManager() *DirectURLManager {
	return &DirectURLManager{}
}

// Name returns the manager identifier
func (m *DirectURLManager) Name() string {
	return "direct"
}

// DiscoverVersions is not supported for direct URLs - they must specify exact versions
func (m *DirectURLManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	return nil, fmt.Errorf("version discovery not supported for direct URLs - specify exact version")
}

// Resolve gets the download URL for a specific version and platform
func (m *DirectURLManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	if pkg.URLTemplate == "" {
		return nil, fmt.Errorf("url_template is required for direct URLs")
	}

	// Resolve asset pattern if specified
	asset := ""
	if len(pkg.AssetPatterns) > 0 {
		platformKey := plat.OS + "-" + plat.Arch
		assetPattern, exists := pkg.AssetPatterns[platformKey]
		if !exists {
			return nil, fmt.Errorf("no asset pattern found for platform %s", platformKey)
		}

		// Template the asset pattern to get the actual asset name
		var err error
		asset, err = depstemplate.TemplateURL(assetPattern, version, plat.OS, plat.Arch)
		if err != nil {
			return nil, fmt.Errorf("failed to template asset pattern: %w", err)
		}
	}

	// Template the URL with asset variable
	downloadURL, err := m.templateURLWithAsset(pkg.URLTemplate, version, plat, asset)
	if err != nil {
		return nil, fmt.Errorf("failed to template URL: %w", err)
	}

	resolution := &types.Resolution{
		Package:     pkg,
		Version:     version,
		Platform:    plat,
		DownloadURL: downloadURL,
		IsArchive:   isArchiveURL(downloadURL),
	}

	// Template checksum URL if available
	if pkg.ChecksumFile != "" {
		var checksumURL string
		var err error

		if strings.HasPrefix(pkg.ChecksumFile, "http://") || strings.HasPrefix(pkg.ChecksumFile, "https://") {
			// Full URL template
			checksumURL, err = m.templateURLWithAsset(pkg.ChecksumFile, version, plat, asset)
		} else {
			// Relative path - construct from download URL directory
			baseURL := filepath.Dir(downloadURL)
			checksumPath, templateErr := m.templateURLWithAsset(pkg.ChecksumFile, version, plat, asset)
			if templateErr != nil {
				err = templateErr
			} else {
				checksumURL = baseURL + "/" + checksumPath
			}
		}

		if err == nil && checksumURL != "" {
			resolution.ChecksumURL = checksumURL
		}
	}

	// Set binary path for archives or direct binaries
	if resolution.IsArchive && pkg.BinaryName != "" {
		resolution.BinaryPath = pkg.BinaryName
	} else if plat.IsWindows() && !strings.HasSuffix(downloadURL, ".exe") {
		// Add .exe extension for Windows binaries
		resolution.BinaryPath = pkg.Name + ".exe"
	}

	return resolution, nil
}

// Install downloads and installs the binary
func (m *DirectURLManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	// For now, return not implemented - the actual installation
	// is handled by the existing deps.Install function
	// TODO: Implement proper installation using download package
	return fmt.Errorf("install method not yet implemented - use existing Install")
}

// GetChecksums is not supported for direct URLs
func (m *DirectURLManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	return nil, fmt.Errorf("checksums not supported for direct URLs")
}

// Verify checks if an installed binary matches expectations
func (m *DirectURLManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	return nil, fmt.Errorf("verify not implemented yet")
}

// templateURL templates a URL with version and platform variables using flanksource/gomplate
func (m *DirectURLManager) templateURL(urlTemplate, version string, plat platform.Platform) (string, error) {
	return depstemplate.TemplateURL(urlTemplate, version, plat.OS, plat.Arch)
}

// templateURLWithAsset templates a URL with version, platform, and asset variables
func (m *DirectURLManager) templateURLWithAsset(urlTemplate, version string, plat platform.Platform, asset string) (string, error) {
	return depstemplate.TemplateURLWithAsset(urlTemplate, version, plat.OS, plat.Arch, asset)
}

// isArchiveURL checks if a URL points to an archive file
func isArchiveURL(url string) bool {
	archiveExtensions := []string{
		".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz",
		".zip", ".7z", ".rar",
	}

	url = strings.ToLower(url)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(url, ext) {
			return true
		}
	}
	return false
}
