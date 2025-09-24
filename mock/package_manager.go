package mock

import (
	"context"
	"fmt"
	"strings"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

// MockPackageManager provides a predictable implementation for testing
type MockPackageManager struct {
	name           string
	versions       []types.Version
	resolveError   error
	installError   error
	checksums      map[string]string
	installedInfos map[string]*types.InstalledInfo
}

// NewMockPackageManager creates a new mock package manager
func NewMockPackageManager(name string) *MockPackageManager {
	return &MockPackageManager{
		name:           name,
		versions:       []types.Version{},
		checksums:      make(map[string]string),
		installedInfos: make(map[string]*types.InstalledInfo),
	}
}

// WithVersions sets the versions that will be returned by DiscoverVersions
func (m *MockPackageManager) WithVersions(versions ...string) *MockPackageManager {
	m.versions = make([]types.Version, len(versions))
	for i, v := range versions {
		m.versions[i] = types.Version{
			Version: v,
			Tag:     "v" + v,
		}
	}
	return m
}

// WithResolveError sets an error that will be returned by Resolve
func (m *MockPackageManager) WithResolveError(err error) *MockPackageManager {
	m.resolveError = err
	return m
}

// WithInstallError sets an error that will be returned by Install
func (m *MockPackageManager) WithInstallError(err error) *MockPackageManager {
	m.installError = err
	return m
}

// WithChecksum sets a checksum for a specific file
func (m *MockPackageManager) WithChecksum(filename, checksum string) *MockPackageManager {
	m.checksums[filename] = checksum
	return m
}

// WithInstalledInfo sets installed info for a binary path
func (m *MockPackageManager) WithInstalledInfo(path string, info *types.InstalledInfo) *MockPackageManager {
	m.installedInfos[path] = info
	return m
}

// PackageManager interface implementation

func (m *MockPackageManager) Name() string {
	return m.name
}

func (m *MockPackageManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	return m.versions, nil
}

func (m *MockPackageManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	if m.resolveError != nil {
		return nil, m.resolveError
	}

	// Create a mock resolution with non-routable URL to prevent network calls
	resolution := &types.Resolution{
		Package:     pkg,
		Version:     version,
		Platform:    plat,
		DownloadURL: fmt.Sprintf("file:///tmp/mock-%s-%s-%s-%s", pkg.Name, version, plat.OS, plat.Arch),
		IsArchive:   strings.Contains(pkg.Name, "archive"),
		BinaryPath:  pkg.BinaryName,
	}

	// Add mock checksum if available
	key := fmt.Sprintf("%s-%s-%s", pkg.Name, plat.OS, plat.Arch)
	if checksum, exists := m.checksums[key]; exists {
		resolution.Checksum = checksum
	} else {
		// Provide a default mock checksum to avoid network calls for checksum calculation
		resolution.Checksum = fmt.Sprintf("sha256:mock-checksum-%s-%s-%s", pkg.Name, plat.OS, plat.Arch)
	}

	return resolution, nil
}

func (m *MockPackageManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	if m.installError != nil {
		return m.installError
	}
	// Mock install always succeeds
	return nil
}

func (m *MockPackageManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	return m.checksums, nil
}

func (m *MockPackageManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	if info, exists := m.installedInfos[binaryPath]; exists {
		return info, nil
	}

	// Return a default installed info
	return &types.InstalledInfo{
		Version:  "mock-version",
		Path:     binaryPath,
		Platform: platform.Current(),
		Checksum: "mock-checksum",
	}, nil
}
