package golang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/flanksource/deps/pkg/manager/github"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

// GoManager implements the PackageManager interface for Go-based tools
// installed via `go install`
type GoManager struct {
	githubManager *github.GitHubReleaseManager
}

// NewGoManager creates a new Go manager
func NewGoManager() *GoManager {
	return &GoManager{
		githubManager: github.NewGitHubReleaseManager(),
	}
}

// Name returns the manager identifier
func (m *GoManager) Name() string {
	return "go"
}

// DiscoverVersions discovers versions from GitHub releases/tags
func (m *GoManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	if pkg.Repo == "" {
		return nil, fmt.Errorf("repo is required for Go packages")
	}

	// Delegate to GitHub manager for version discovery
	return m.githubManager.DiscoverVersions(ctx, pkg, plat, limit)
}

// Resolve gets the installation metadata for a specific version
func (m *GoManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	resolution := &types.Resolution{
		Package:  pkg,
		Version:  version,
		Platform: plat,
		// For Go packages, we don't download anything - Install handles everything
		DownloadURL: "",
		ChecksumURL: "",
		IsArchive:   false,
	}

	return resolution, nil
}

// Install downloads and installs a Go binary via go install
func (m *GoManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	importPath, err := m.getImportPath(resolution.Package)
	if err != nil {
		return err
	}

	installTarget := fmt.Sprintf("%s@%s", importPath, resolution.Version)

	// Set up GOBIN to install to the correct location
	gobin := opts.BinDir
	if gobin == "" {
		return fmt.Errorf("bin_dir is required for Go package installation")
	}

	// Ensure bin directory exists
	if err := os.MkdirAll(gobin, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Run go install
	cmd := exec.CommandContext(ctx, "go", "install", installTarget)
	cmd.Env = append(os.Environ(), fmt.Sprintf("GOBIN=%s", gobin))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}

	return nil
}

// GetChecksums is not applicable for Go packages (installed from source)
func (m *GoManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	// Go packages are built from source, so checksums are not applicable
	return map[string]string{}, nil
}

// Verify checks if an installed Go binary matches expectations
func (m *GoManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("binary not found: %s", binaryPath)
	}

	// Try to get version if version command is specified
	version := "unknown"
	if pkg.VersionCommand != "" {
		cmd := exec.CommandContext(ctx, binaryPath, strings.Split(pkg.VersionCommand, " ")...)
		output, err := cmd.CombinedOutput()
		if err == nil {
			version = strings.TrimSpace(string(output))
		}
	}

	return &types.InstalledInfo{
		Version:  version,
		Path:     binaryPath,
		Checksum: "", // Not applicable for Go packages
	}, nil
}

// Helper methods

func (m *GoManager) getImportPath(pkg types.Package) (string, error) {
	if pkg.Extra == nil {
		return "", fmt.Errorf("go package requires 'extra' configuration with 'import_path'")
	}

	importPath, exists := pkg.Extra["import_path"]
	if !exists {
		return "", fmt.Errorf("import_path is required in extra configuration for Go packages")
	}

	return fmt.Sprintf("%v", importPath), nil
}

func (m *GoManager) getBinaryName(pkg types.Package) string {
	// Extract binary name from import path (last component)
	importPath, err := m.getImportPath(pkg)
	if err != nil {
		return pkg.Name
	}

	parts := strings.Split(importPath, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return pkg.Name
}
