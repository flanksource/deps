package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/pipeline"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	versionpkg "github.com/flanksource/deps/pkg/version"
)

// InstallPreview describes the no-side-effect install plan for a package.
type InstallPreview struct {
	Package          types.Package
	Manager          string
	Plugin           string
	Platform         platform.Platform
	RequestedInput   string
	RequestedVersion string
	ResolvedVersion  string
	EffectiveVersion string
	AlreadyInstalled bool
	ExistingVersion  string
	ExistingPath     string
	ExistingSource   string
	Resolution       *types.Resolution
}

func (p *InstallPreview) DisplayVersion() string {
	if p == nil {
		return ""
	}
	if p.EffectiveVersion != "" {
		return p.EffectiveVersion
	}
	if p.ResolvedVersion != "" {
		return p.ResolvedVersion
	}
	return p.RequestedVersion
}

func (p *InstallPreview) InstallMethod() string {
	if p == nil {
		return ""
	}
	switch {
	case p.Plugin != "":
		return "plugin"
	case p.Resolution == nil:
		return ""
	case p.Resolution.DownloadURL == "":
		return "manager"
	default:
		return "download"
	}
}

func (i *Installer) Preview(name, version string, t *task.Task) (*InstallPreview, error) {
	if t == nil {
		t = &task.Task{}
	}

	if i.depsConfig != nil {
		if pkg, exists := i.depsConfig.Registry[name]; exists {
			return i.previewPackageInstallation(context.Background(), name, version, pkg, t)
		}
	}

	if isGitHubRepoPattern(name) {
		pkg := createGitHubPackage(name)
		return i.previewPackageInstallation(context.Background(), pkg.Name, version, pkg, t)
	}

	return nil, fmt.Errorf("tool %s not found in registry - please add it to deps.yaml registry section", name)
}

func (i *Installer) previewPackageInstallation(ctx context.Context, name, version string, pkg types.Package, t *task.Task) (*InstallPreview, error) {
	preview := &InstallPreview{
		Package:          pkg,
		Manager:          pkg.Manager,
		Platform:         i.getPlatform(),
		RequestedInput:   version,
		RequestedVersion: version,
	}
	if err := i.validateGitHubFilters(name, pkg); err != nil {
		return nil, err
	}

	if preview.RequestedVersion == "" {
		preview.RequestedVersion = "latest"
	}

	if handler := i.plugins.FindHandler(name, pkg); handler != nil {
		preview.Plugin = handler.Name()
		preview.Manager = "plugin"
		return preview, nil
	}

	mgr, err := i.managers.GetForPackage(pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to get package manager for %s: %w", name, err)
	}

	preview.Manager = mgr.Name()

	requestedVersion := version
	if requestedVersion == "any" {
		if path, source, found := i.findExistingAnyInstallation(name, pkg); found {
			preview.AlreadyInstalled = true
			preview.ExistingPath = path
			preview.ExistingSource = source
			return preview, nil
		}
		requestedVersion = "latest"
	}
	if requestedVersion == "" {
		requestedVersion = "latest"
	}
	preview.RequestedVersion = requestedVersion

	markExisting := func(existingVersion string) *InstallPreview {
		preview.AlreadyInstalled = true
		preview.ExistingVersion = existingVersion
		if path, ok := i.getInstalledPath(name, pkg); ok {
			preview.ExistingPath = path
		}
		return preview
	}

	if !i.options.Force && len(i.options.AssetFilters) == 0 {
		existingVersion := i.checkExistingInstallation(t, name, pkg, requestedVersion)
		if existingVersion != "" && versionpkg.Normalize(existingVersion) == versionpkg.Normalize(requestedVersion) {
			return markExisting(existingVersion), nil
		}
	}

	t.SetDescription(fmt.Sprintf("Resolving version %s", requestedVersion))
	resolveCtx := i.managerContext(ctx)
	resolvedVersion, err := i.resolveVersionConstraint(resolveCtx, mgr, pkg, requestedVersion, t)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve version constraint for %s: %w", name, err)
	}
	preview.ResolvedVersion = resolvedVersion

	if !i.options.Force && len(i.options.AssetFilters) == 0 {
		if existingVersion := i.checkExistingInstallation(t, name, pkg, resolvedVersion); existingVersion != "" {
			return markExisting(existingVersion), nil
		}
	}

	resolution, err := mgr.Resolve(resolveCtx, pkg, resolvedVersion, preview.Platform)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve package %s: %w", name, err)
	}
	// Package.Extract overrides archive auto-detection (URLs without an
	// archive suffix, e.g. get.trivy.dev/trivy?type=tar.gz)
	if pkg.Extract != nil {
		resolution.IsArchive = *pkg.Extract
	}
	preview.Resolution = resolution
	preview.EffectiveVersion = resolvedVersion

	if mgr.Name() != "github_build" && resolution.GitHubAsset != nil && resolution.GitHubAsset.Tag != "" {
		if actualVersion := versionpkg.Normalize(resolution.GitHubAsset.Tag); actualVersion != resolvedVersion {
			t.SetName(fmt.Sprintf("%s@%s", name, actualVersion))
			t.SetDescription(fmt.Sprintf("Resolved %s -> %s", resolvedVersion, actualVersion))
			preview.EffectiveVersion = actualVersion
		}
	}

	return preview, nil
}

func (i *Installer) managerContext(ctx context.Context) context.Context {
	ctx = manager.WithStrictChecksum(ctx, i.options.StrictChecksum)
	ctx = manager.WithIterateVersions(ctx, i.options.IterateVersions)
	ctx = manager.WithAssetFilters(ctx, i.options.AssetFilters)
	return manager.WithReleaseFilters(ctx, i.options.ReleaseFilters)
}

func (i *Installer) findExistingAnyInstallation(name string, pkg types.Package) (string, string, bool) {
	binaryName := name
	if pkg.BinaryName != "" {
		binaryName = pkg.BinaryName
	}

	if path, err := exec.LookPath(binaryName); err == nil {
		return path, "PATH", true
	}

	binPath := filepath.Join(i.options.BinDir, binaryName)
	if _, err := os.Stat(binPath); err == nil {
		return binPath, "bin-dir", true
	}

	return "", "", false
}

func (i *Installer) checkExistingInstallation(t *task.Task, name string, pkg types.Package, requestedVersion string) string {
	if i.options.ArchOverride != "" {
		binaryName := name
		if pkg.BinaryName != "" {
			binaryName = pkg.BinaryName
		}
		if i.options.OSOverride == "windows" && filepath.Ext(binaryName) == "" {
			binaryName += ".exe"
		}
		binPath := filepath.Join(i.options.BinDir, binaryName)
		if nativeArch := pipeline.DetectBinaryArch(binPath); nativeArch != "" && !archMatches(nativeArch, i.options.ArchOverride) {
			t.Debugf("Existing %s is %s but %s requested, reinstalling", binaryName, nativeArch, i.options.ArchOverride)
			return ""
		}
	}

	return versionpkg.CheckExistingInstallation(t, name, pkg, requestedVersion, i.options.BinDir, i.options.OSOverride)
}

func (i *Installer) getInstalledPath(name string, pkg types.Package) (string, bool) {
	binaryName := name
	if pkg.BinaryName != "" {
		binaryName = pkg.BinaryName
	}

	if i.options.OSOverride == "windows" && filepath.Ext(binaryName) == "" {
		binaryName += ".exe"
	}

	path := filepath.Join(i.options.BinDir, binaryName)
	if _, err := os.Stat(path); err == nil {
		return path, true
	}

	return "", false
}
