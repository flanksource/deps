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

	checkExisting := !i.options.Force && len(i.options.AssetFilters) == 0 && i.existingArchMatches(name, pkg, t)

	// An alias names whatever is current at resolution time, so there is nothing to compare
	// the installed version against yet — that check has to wait until Resolve() reports the
	// concrete version behind it. A pinned version is compared here so an already-installed
	// package needs no network at all.
	if checkExisting && !versionpkg.IsAlias(resolvedVersion) {
		if i.markAlreadyInstalled(preview, name, pkg, resolvedVersion, t) {
			return preview, nil
		}
		checkExisting = false
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

	if actualVersion := concreteResolvedVersion(mgr, resolution, resolvedVersion); actualVersion != "" && actualVersion != resolvedVersion {
		t.SetName(fmt.Sprintf("%s@%s", name, actualVersion))
		t.SetDescription(fmt.Sprintf("Resolved %s -> %s", resolvedVersion, actualVersion))
		preview.EffectiveVersion = actualVersion
	}

	if checkExisting {
		if versionpkg.IsAlias(preview.EffectiveVersion) {
			t.V(3).Infof("%s resolved %s to no concrete version; installing without an existing-installation check", name, resolvedVersion)
		} else if i.markAlreadyInstalled(preview, name, pkg, preview.EffectiveVersion, t) {
			return preview, nil
		}
	}

	return preview, nil
}

// concreteResolvedVersion returns the real version a resolution landed on, which is the only
// thing an installed binary can be compared against when the request was an alias.
func concreteResolvedVersion(mgr manager.PackageManager, resolution *types.Resolution, resolvedVersion string) string {
	if mgr.Name() != "github_build" && resolution.GitHubAsset != nil && resolution.GitHubAsset.Tag != "" {
		return versionpkg.Normalize(resolution.GitHubAsset.Tag)
	}
	// Deterministic resolutions (checksum_file + fixed asset pattern) carry no asset metadata
	// but do report the tag they resolved the alias to.
	if versionpkg.IsAlias(resolvedVersion) && resolution.Version != "" && !versionpkg.IsAlias(resolution.Version) {
		return versionpkg.Normalize(resolution.Version)
	}
	return ""
}

// existingArchMatches reports whether an existing binary is usable for the requested arch.
// A binary built for another architecture has to be reinstalled whatever its version says.
func (i *Installer) existingArchMatches(name string, pkg types.Package, t *task.Task) bool {
	if i.options.ArchOverride == "" {
		return true
	}

	binaryName := name
	if pkg.BinaryName != "" {
		binaryName = pkg.BinaryName
	}
	nativeArch := pipeline.DetectBinaryArch(filepath.Join(i.options.BinDir, binaryName))
	if nativeArch == "" || archMatches(nativeArch, i.options.ArchOverride) {
		return true
	}

	t.Debugf("Existing %s is %s but %s requested, reinstalling", binaryName, nativeArch, i.options.ArchOverride)
	return false
}

// markAlreadyInstalled fills in the existing-installation fields when the binary on disk
// already satisfies want, and reports whether it did.
func (i *Installer) markAlreadyInstalled(preview *InstallPreview, name string, pkg types.Package, want string, t *task.Task) bool {
	existingVersion := versionpkg.CheckExistingInstallation(t, name, pkg, want, i.options.BinDir, i.options.OSOverride)
	if existingVersion == "" {
		return false
	}

	preview.AlreadyInstalled = true
	preview.ExistingVersion = existingVersion
	if path, ok := i.getInstalledPath(name, pkg); ok {
		preview.ExistingPath = path
	}
	return true
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
