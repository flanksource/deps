package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/deps/download"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/extract"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/pipeline"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/plugin"
	_ "github.com/flanksource/deps/pkg/plugin/builtin" // Register built-in plugins
	"github.com/flanksource/deps/pkg/system"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/utils"
	versionpkg "github.com/flanksource/deps/pkg/version"
)

// ToolSpec represents a tool with optional version
type ToolSpec struct {
	Name    string
	Version string
}

// DownloadResult holds information about a completed download
type DownloadResult struct {
	ChecksumUsed    string
	ChecksumType    string
	ChecksumSources []string
}

// Installer handles package installation with unified API
type Installer struct {
	managers   *manager.Registry
	plugins    *plugin.Registry
	options    InstallOptions
	depsConfig *types.DepsConfig
}

// New creates a new installer with the given options
func New(opts ...InstallOption) *Installer {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	return &Installer{
		managers:   NewManagerRegistry(),
		plugins:    GetPluginRegistry(),
		options:    options,
		depsConfig: nil, // Will be set via WithDepsConfig
	}
}

// NewWithConfig creates a new installer with the given options and config
func NewWithConfig(depsConfig *types.DepsConfig, opts ...InstallOption) *Installer {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	return &Installer{
		managers:   NewManagerRegistry(),
		plugins:    GetPluginRegistry(),
		options:    options,
		depsConfig: depsConfig,
	}
}

// shouldSkipCleanup returns true if temporary files should be preserved
// This happens when the user specified a custom tmp-dir (different from os.TempDir())
func (i *Installer) shouldSkipCleanup() bool {
	return i.options.TmpDir != os.TempDir()
}

// ParseTools parses tool specifications from string arguments
func ParseTools(args []string) []ToolSpec {
	var tools []ToolSpec
	for _, arg := range args {
		parts := strings.Split(arg, "@")
		tool := ToolSpec{Name: parts[0]}
		if len(parts) > 1 {
			tool.Version = parts[1]
		}
		tools = append(tools, tool)
	}
	return tools
}

// Install installs a single tool with task progress tracking
func (i *Installer) Install(name, version string, t *task.Task) error {
	return i.installTool(ToolSpec{Name: name, Version: version}, t)
}

// InstallMultiple installs multiple tools
func (i *Installer) InstallMultiple(tools []ToolSpec) error {
	// Start tasks for each tool
	for _, tool := range tools {
		t := tool // Capture for closure
		taskName := t.Name
		if t.Version != "" {
			taskName = fmt.Sprintf("%s@%s", t.Name, t.Version)
		}

		task.StartTask(taskName, func(ctx flanksourceContext.Context, task *task.Task) (interface{}, error) {
			if err := i.installTool(t, task); err != nil {
				return nil, fmt.Errorf("failed to install %s: %w", t.Name, err)
			}
			return nil, nil
		})
	}

	return nil
}

// InstallFromConfig installs all dependencies from deps.yaml
func (i *Installer) InstallFromConfig(t *task.Task) error {

	// Load global config (defaults + user)
	depsConfig := config.GetGlobalRegistry()

	if err := config.ValidateConfig(depsConfig); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Try to load deps-lock.yaml for locked versions
	lockFile, lockErr := config.LoadLockFile("")
	if lockErr != nil {
		t.Debugf("No lock file found (%v), using version constraints from deps.yaml", lockErr)
	}

	// Create tasks for each dependency
	for name, constraint := range depsConfig.Dependencies {
		depName := name // Capture for closure
		depConstraint := constraint

		var version string
		var useNewPackageManager bool

		// Check if we have this dependency in the lock file
		if lockFile != nil {
			if lockEntry, exists := lockFile.Dependencies[depName]; exists {
				version = lockEntry.Version
				useNewPackageManager = true
			}
		}

		// If not in lock file, check if it's in the new registry format
		if version == "" {
			if _, exists := depsConfig.Registry[depName]; exists {
				// Use new package manager system
				useNewPackageManager = true
				// Version resolution will be done inside the task where we have access to logging
			}
		}

		// Create installation task
		taskName := depName
		if version != "" {
			taskName = fmt.Sprintf("%s@%s", depName, version)
		}

		task.StartTask(taskName, func(ctx flanksourceContext.Context, task *task.Task) (interface{}, error) {
			if useNewPackageManager {
				pkg := depsConfig.Registry[depName]

				// Resolve version constraint if not already resolved from lock file
				resolvedVersion := version
				if resolvedVersion == "" {
					task.V(3).Infof("Resolving version constraint '%s' for %s", depConstraint, depName)
					mgr, err := i.managers.GetForPackage(pkg)
					if err != nil {
						return nil, fmt.Errorf("failed to get package manager for %s: %w", depName, err)
					}

					resolvedVersion, err = i.resolveVersionConstraint(ctx.Context, mgr, pkg, depConstraint, task)
					if err != nil {
						return nil, fmt.Errorf("failed to resolve version constraint for %s: %w", depName, err)
					}
					task.Infof("Resolved %s version: %s -> %s", depName, depConstraint, resolvedVersion)
				}

				return nil, i.installWithNewPackageManager(ctx.Context, depName, resolvedVersion, pkg, task)
			} else {
				return nil, fmt.Errorf("dependency %s not found in registry - please add it to deps.yaml registry section", depName)
			}
		})
	}

	return nil
}

// installTool handles the installation of a single tool
func (i *Installer) installTool(tool ToolSpec, t *task.Task) error {
	// Check if package is defined in new registry format first
	if i.depsConfig != nil {
		if pkg, exists := i.depsConfig.Registry[tool.Name]; exists {
			return i.installWithNewPackageManager(context.Background(), tool.Name, tool.Version, pkg, t)
		}
	}

	// No fallback - package must be in registry
	return fmt.Errorf("tool %s not found in registry - please add it to deps.yaml registry section", tool.Name)
}

// installWithNewPackageManager installs using the new package manager system
func (i *Installer) installWithNewPackageManager(ctx context.Context, name, version string, pkg types.Package, t *task.Task) error {
	// First check if there's a plugin that can handle this package
	if handler := i.plugins.FindHandler(name, pkg); handler != nil {
		pluginOpts := plugin.InstallOptions{
			BinDir:       i.options.BinDir,
			Force:        i.options.Force,
			SkipChecksum: i.options.SkipChecksum,
			Debug:        i.options.Debug,
			OSOverride:   i.options.OSOverride,
			ArchOverride: i.options.ArchOverride,
		}

		if err := handler.Install(flanksourceContext.NewContext(ctx), name, version, pkg, pluginOpts, t); err != nil {
			return fmt.Errorf("failed to install %s using plugin: %w", name, err)
		}
		return nil
	}

	// Get the appropriate manager for this package
	mgr, err := i.managers.GetForPackage(pkg)
	if err != nil {
		return fmt.Errorf("failed to get package manager for %s: %w", name, err)
	}

	t.Debugf("Install: selected package manager %s for package %s", mgr.Name(), name)

	// Step 1: Resolve and validate version
	resolvedVersion, alreadyInstalled, err := i.resolveAndValidateVersion(ctx, mgr, name, version, pkg, t)
	if err != nil {
		return err
	}
	if alreadyInstalled {
		return nil
	}

	// Step 2: Download package
	resolution, downloadPath, err := i.downloadPackage(ctx, mgr, name, resolvedVersion, pkg, t)
	if err != nil {
		return err
	}

	// Step 3: Handle installation based on file type (installer, archive, or direct binary)
	var finalPath string

	// Check if this is a system installer first
	if extract.IsSystemInstaller(downloadPath) {
		finalPath, err = i.handleSystemInstaller(downloadPath, name, t)
		if err != nil {
			return err
		}
	} else if resolution.IsArchive {
		finalPath, err = i.handleArchiveInstallation(downloadPath, name, resolvedVersion, resolution, pkg, t)
		if err != nil {
			return err
		}
	} else {
		finalPath, err = i.handleDirectBinaryInstallation(downloadPath, name)
		if err != nil {
			return err
		}
	}

	// Step 4: Finalize installation
	return i.finalizeInstallation(name, resolvedVersion, finalPath, pkg, t)
}

// moveExtractedDirectory moves an extracted archive directory to the target location
// It finds the first directory in workDir and moves it to targetDir, renaming as needed
func (i *Installer) moveExtractedDirectory(workDir, targetDir string, t *task.Task) error {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return fmt.Errorf("failed to read extracted directory: %w", err)
	}

	// Filter out hidden files (starting with '.')
	var visibleEntries []os.DirEntry
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			visibleEntries = append(visibleEntries, entry)
		}
	}

	if len(visibleEntries) == 0 {
		return fmt.Errorf("no visible files or directories found in extracted archive")
	}

	if len(visibleEntries) == 1 && visibleEntries[0].IsDir() {
		// Single directory: move it (existing behavior for compatibility)
		extractedDir := filepath.Join(workDir, visibleEntries[0].Name())
		return i.moveSingleDirectory(extractedDir, targetDir, t)
	} else {
		// Multiple items: move all contents to target directory
		return i.moveAllContents(workDir, targetDir, visibleEntries, t)
	}
}

// moveSingleDirectory moves a single extracted directory to the target location (existing behavior)
func (i *Installer) moveSingleDirectory(extractedDir, targetDir string, t *task.Task) error {
	t.V(3).Infof("Moving single extracted directory from %s to %s", extractedDir, targetDir)

	// Remove target if it exists (for updates)
	if _, err := os.Stat(targetDir); err == nil {
		t.V(3).Infof("Removing existing directory: %s", targetDir)
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("failed to remove existing directory: %w", err)
		}
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(targetDir)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Move (rename) extracted directory to final location
	if err := os.Rename(extractedDir, targetDir); err != nil {
		return fmt.Errorf("failed to move directory: %w", err)
	}

	return nil
}

// moveAllContents moves the entire extraction directory to become the target directory
func (i *Installer) moveAllContents(workDir, targetDir string, entries []os.DirEntry, t *task.Task) error {
	t.V(3).Infof("Moving entire directory (%d items) from %s to %s", len(entries), workDir, targetDir)

	// Remove target if it exists (for updates)
	if _, err := os.Stat(targetDir); err == nil {
		t.V(3).Infof("Removing existing directory: %s", targetDir)
		if err := os.RemoveAll(targetDir); err != nil {
			return fmt.Errorf("failed to remove existing directory: %w", err)
		}
	}

	// Ensure parent directory exists
	parentDir := filepath.Dir(targetDir)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Move entire directory as single atomic operation
	if err := os.Rename(workDir, targetDir); err != nil {
		return fmt.Errorf("failed to move directory %s to %s: %w", workDir, targetDir, err)
	}

	t.V(3).Infof("Successfully moved entire directory to %s", targetDir)
	return nil
}

// resolveAndValidateVersion handles version resolution and existing installation check
func (i *Installer) resolveAndValidateVersion(ctx context.Context, mgr manager.PackageManager, name string, version string, pkg types.Package, t *task.Task) (string, bool, error) {
	// If no version specified, resolve it
	if version == "" {
		version = "latest"
	}

	t.SetDescription(fmt.Sprintf("Resolving version %s", version))

	// Resolve version constraint to specific version
	resolvedVersion, err := i.resolveVersionConstraint(ctx, mgr, pkg, version, t)
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve version constraint for %s: %w", name, err)
	}

	t.Debugf("Resolved %s version: %s -> %s", name, version, resolvedVersion)

	// Update task name to include the resolved version
	t.SetName(fmt.Sprintf("%s@%s", name, resolvedVersion))

	// Check if binary already exists with correct version (unless force flag is set)
	if !i.options.Force {
		if existingVersion := versionpkg.CheckExistingInstallation(t, name, pkg, resolvedVersion, i.options.BinDir, i.options.OSOverride); existingVersion != "" {
			t.Infof("✓ %s@%s is already installed", name, resolvedVersion)
			t.Success()
			return resolvedVersion, true, nil // true indicates already installed
		}
	}

	return resolvedVersion, false, nil // false indicates needs installation
}

// downloadPackage handles package resolution and download
func (i *Installer) downloadPackage(ctx context.Context, mgr manager.PackageManager, name, resolvedVersion string, pkg types.Package, t *task.Task) (*types.Resolution, string, error) {
	// Create platform info (now using global overrides set from CLI)
	plat := platform.Current()

	// Resolve the package
	t.Debugf("Fetching download URL for %s@%s using %s", name, resolvedVersion, mgr.Name())
	t.V(3).Infof("Package details: Name=%s, Repo=%s, Manager=%s", pkg.Name, pkg.Repo, mgr.Name())
	t.V(3).Infof("Asset patterns: %+v", pkg.AssetPatterns)
	t.V(3).Infof("Version expr: %s", pkg.VersionExpr)
	t.V(3).Infof("Platform: %s/%s", plat.OS, plat.Arch)

	t.Debugf("Calling mgr.Resolve with version=%s, platform=%s/%s", resolvedVersion, plat.OS, plat.Arch)
	resolution, err := mgr.Resolve(ctx, pkg, resolvedVersion, plat)
	if err != nil {
		t.Debugf("mgr.Resolve failed: %v", err)
		return nil, "", fmt.Errorf("failed to resolve package %s: %w", name, err)
	}
	t.Debugf("mgr.Resolve succeeded")

	// Add detailed logging for debugging URL construction
	t.Debugf("Resolution details: URL=%s, IsArchive=%t, BinaryPath=%s",
		resolution.DownloadURL, resolution.IsArchive, resolution.BinaryPath)
	t.V(3).Infof("Package resolution: version=%s, platform=%s/%s",
		resolvedVersion, plat.OS, plat.Arch)

	t.Infof("Downloading %s@%s (%s/%s) from %s", name, resolvedVersion, plat.OS, plat.Arch, resolution.DownloadURL)

	// Create bin directory if it doesn't exist
	if err := os.MkdirAll(i.options.BinDir, 0755); err != nil {
		return nil, "", fmt.Errorf("failed to create bin directory: %w", err)
	}

	var downloadPath string

	// Get file extension to determine download strategy
	ext := extract.GetExtension(resolution.DownloadURL)
	isSystemInstaller := strings.ToLower(ext) == ".pkg" || strings.ToLower(ext) == ".msi"

	if resolution.IsArchive || isSystemInstaller {
		// Download to temp file with extension preserved (for archives and installers)
		downloadPath = filepath.Join(i.options.TmpDir, fmt.Sprintf("deps-%s-%s%s", name, resolvedVersion, ext))

		// Download with checksum verification if available
		if err := i.downloadWithChecksum(resolution.DownloadURL, downloadPath, resolution.ChecksumURL, resolution, t); err != nil {
			return nil, "", err
		}
	} else {
		// Direct binary download - download directly to final location
		downloadPath = filepath.Join(i.options.BinDir, name)
		t.SetDescription("Downloading")

		if err := i.downloadWithChecksum(resolution.DownloadURL, downloadPath, resolution.ChecksumURL, resolution, t); err != nil {
			return nil, "", fmt.Errorf("failed to download %s: %w", name, err)
		}
	}

	return resolution, downloadPath, nil
}

// executePostProcessing runs the post-processing pipeline if configured
func (i *Installer) executePostProcessing(pkg types.Package, workDir, targetPath string, t *task.Task) error {
	if len(pkg.PostProcess) == 0 {
		return nil // No post-processing needed
	}

	t.V(3).Infof("Applying post-process pipeline: %v", pkg.PostProcess)

	// Create CEL pipeline from expressions
	celPipeline := pipeline.NewCELPipeline(pkg.PostProcess)
	if celPipeline == nil {
		return fmt.Errorf("failed to create CEL pipeline from expressions: %v", pkg.PostProcess)
	}

	// Execute the CEL pipeline on the extracted contents
	evaluator := pipeline.NewCELPipelineEvaluator(workDir, targetPath, i.options.TmpDir, t, i.options.Debug)
	if err := evaluator.Execute(celPipeline); err != nil {
		return err
	}

	return nil
}

// finalizeInstallation makes the binary executable and reports success
func (i *Installer) finalizeInstallation(name, resolvedVersion, finalPath string, pkg types.Package, t *task.Task) error {
	// Make executable (skip for directories and when post-process was used)
	if len(pkg.PostProcess) == 0 && pkg.Mode != "directory" {
		fileInfo, err := os.Stat(finalPath)
		if err == nil && !fileInfo.IsDir() {
			if err := os.Chmod(finalPath, 0755); err != nil {
				return fmt.Errorf("failed to make binary executable: %w", err)
			}
		}
	}

	// Create symlinks for directory-mode packages
	if pkg.Mode == "directory" && len(pkg.Symlinks) > 0 {
		t.SetDescription("Creating symlinks")
		if err := i.createSymlinks(finalPath, i.options.BinDir, pkg.Symlinks, t); err != nil {
			return fmt.Errorf("failed to create symlinks: %w", err)
		}
	}

	// Mark task successful only after all operations (including post-processing) complete
	t.Infof("✓ Successfully installed %s@%s to %s", name, resolvedVersion, finalPath)
	t.Success()

	return nil
}

// handleArchiveInstallation processes an archive download (extraction, binary finding, post-processing)
func (i *Installer) handleArchiveInstallation(downloadPath, name, resolvedVersion string, resolution *types.Resolution, pkg types.Package, t *task.Task) (string, error) {
	// Auto-extract archive to working directory immediately after download
	workDir := filepath.Join(i.options.TmpDir, fmt.Sprintf("deps-extract-%s-%s", name, resolvedVersion))

	if _, extractErr := extract.Extract(downloadPath, workDir, t, extract.WithFullExtract()); extractErr != nil {
		return "", extractErr
	}

	// Set up cleanup
	cleanup := NewCleanupManager(i.options.Debug, i.shouldSkipCleanup(), t)
	cleanup.AddDirectory(workDir)
	cleanup.AddFile(downloadPath)
	defer cleanup.GetCleanupFunc()()

	var finalPath string

	// Use resolution.Package since managers can modify the mode
	resolvedPkg := resolution.Package

	// Handle directory mode installation
	if resolvedPkg.Mode == "directory" {
		t.SetDescription("Installing directory")

		// Move entire directory to app-dir/{package-name}/
		targetDir := filepath.Join(i.options.AppDir, resolvedPkg.Name)
		if err := i.moveExtractedDirectory(workDir, targetDir, t); err != nil {
			return "", fmt.Errorf("failed to move directory: %w", err)
		}

		finalPath = targetDir

		// Run post-process operations inside the moved directory (sandboxed)
		if err := i.executePostProcessing(resolvedPkg, targetDir, targetDir, t); err != nil {
			return "", fmt.Errorf("failed to execute post-process pipeline: %w", err)
		}

	} else {
		finalPath = filepath.Join(i.options.BinDir, name)

		// Check if package has post-processing pipeline
		if len(resolvedPkg.PostProcess) > 0 {
			// Execute the CEL pipeline on the extracted contents
			if err := i.executePostProcessing(resolvedPkg, workDir, finalPath, t); err != nil {
				return "", err
			}

			// Make executable after post-processing completes
			if fileInfo, err := os.Stat(finalPath); err == nil && !fileInfo.IsDir() {
				if err := os.Chmod(finalPath, 0755); err != nil {
					return "", fmt.Errorf("failed to make binary executable: %w", err)
				}
			}

		} else {
			// Regular binary search in already extracted archive
			t.SetDescription("Searching for binary")

			binaryPath, err := extract.FindBinaryInDir(workDir, resolution.BinaryPath, t)
			if err != nil {
				return "", fmt.Errorf("failed to find binary %s: %w", name, err)
			}

			utils.LogFileFound(t, binaryPath, "binary")

			// Copy binary to final destination
			t.SetDescription("Installing binary")

			if err := utils.CopyFile(binaryPath, finalPath); err != nil {
				return "", fmt.Errorf("failed to install binary: %w", err)
			}
		}
	}

	return finalPath, nil
}

// handleSystemInstaller handles system installer files (.pkg/.msi)
func (i *Installer) handleSystemInstaller(installerPath, name string, t *task.Task) (string, error) {
	opts := &system.SystemInstallOptions{
		ToolName: name,
		Silent:   false, // Always show warnings for system installations
		Task:     t,
	}

	result, err := system.InstallSystemPackage(installerPath, "", opts)
	if err != nil {
		return "", fmt.Errorf("failed to install system package %s: %w", name, err)
	}

	// If we found the binary path, return it
	if result.BinaryPath != "" {
		return result.BinaryPath, nil
	}

	// If installation succeeded but we couldn't find the binary,
	// create a marker file in bin dir to indicate successful installation
	markerPath := filepath.Join(i.options.BinDir, name+".installed")
	if err := os.MkdirAll(i.options.BinDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin directory: %w", err)
	}

	installerInfo := fmt.Sprintf("System installer: %s\nInstall path: %s", installerPath, result.InstallPath)
	if err := os.WriteFile(markerPath, []byte(installerInfo), 0644); err != nil {
		return "", fmt.Errorf("failed to create installation marker: %w", err)
	}

	t.Infof("✓ System installation completed. Binary may be available in PATH as '%s'", name)
	return markerPath, nil
}

// handleDirectBinaryInstallation handles direct binary downloads (no extraction needed)
func (i *Installer) handleDirectBinaryInstallation(downloadPath, name string) (string, error) {
	// For direct binary downloads, downloadPath is already the final path
	finalPath := filepath.Join(i.options.BinDir, name)

	// The download method already downloaded to the correct location
	// Just return the path where it was downloaded
	return finalPath, nil
}

// createSymlinks creates symlinks from app directory to bin directory based on glob patterns
func (i *Installer) createSymlinks(appPath, binDir string, patterns []string, t *task.Task) error {
	if len(patterns) == 0 {
		return nil
	}

	t.V(3).Infof("Creating symlinks from %s to %s", appPath, binDir)

	// Ensure bin directory exists
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	for _, pattern := range patterns {
		// Resolve pattern relative to app directory
		fullPattern := filepath.Join(appPath, pattern)
		t.V(3).Infof("Resolving symlink pattern: %s", fullPattern)

		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			return fmt.Errorf("failed to glob pattern %s: %w", pattern, err)
		}

		if len(matches) == 0 {
			t.Warnf("No files matched symlink pattern: %s", pattern)
			continue
		}

		for _, match := range matches {
			// Skip directories
			info, err := os.Stat(match)
			if err != nil {
				t.Warnf("Failed to stat %s: %v", match, err)
				continue
			}
			if info.IsDir() {
				continue
			}

			// Get the basename for the symlink
			linkName := filepath.Base(match)
			linkPath := filepath.Join(binDir, linkName)

			// Remove existing symlink or file if it exists
			if _, err := os.Lstat(linkPath); err == nil {
				if err := os.Remove(linkPath); err != nil {
					t.Warnf("Failed to remove existing symlink %s: %v", linkPath, err)
					continue
				}
			}

			// Create symlink
			if err := os.Symlink(match, linkPath); err != nil {
				t.Warnf("Failed to create symlink %s -> %s: %v", linkPath, match, err)
				continue
			}

			t.V(3).Infof("Created symlink: %s -> %s", linkPath, match)
		}
	}

	return nil
}

// resolveVersionConstraint resolves a version constraint to a specific version
func (i *Installer) resolveVersionConstraint(ctx context.Context, mgr manager.PackageManager, pkg types.Package, constraint string, t *task.Task) (string, error) {
	// Use the centralized version resolver
	resolver := versionpkg.NewResolver(mgr)
	return resolver.ResolveConstraint(ctx, pkg, constraint, platform.Current())
}

// downloadWithChecksum attempts to download with checksum verification
// using the provided checksum URL first, then falling back to URL pattern detection
func (i *Installer) downloadWithChecksum(url, dest, checksumURL string, resolution *types.Resolution, t *task.Task) error {
	// Skip checksum verification if requested
	if i.options.SkipChecksum {
		t.Debugf("Skipping checksum verification (--skip-checksum)")
		return download.Download(url, dest, t, download.WithCacheDir(i.options.CacheDir))
	}

	// Try the provided checksum URL if configured
	if checksumURL != "" {
		t.V(3).Infof("Using configured checksum URL: %s", checksumURL)

		// Split comma-separated checksum URLs
		checksumURLs := strings.Split(checksumURL, ",")
		for i, url := range checksumURLs {
			checksumURLs[i] = strings.TrimSpace(url)
		}

		// Get checksum expression from package configuration
		checksumExpr := resolution.Package.ChecksumExpr

		var err error
		if len(checksumURLs) > 1 || checksumExpr != "" {
			// Parse the logical names from checksum file configuration
			// The checksumURL contains the original config like "checksums,checksums_hashes_order"
			checksumNames := strings.Split(resolution.Package.ChecksumFile, ",")
			for i, name := range checksumNames {
				checksumNames[i] = strings.TrimSpace(name)
			}

			// Use multi-file checksum with CEL support
			err = download.Download(url, dest, t, download.WithChecksumURLsAndNames(checksumURLs, checksumNames, checksumExpr), download.WithCacheDir(i.options.CacheDir))
		} else {
			// Use single checksum file
			err = download.Download(url, dest, t, download.WithChecksumURL(checksumURL), download.WithCacheDir(i.options.CacheDir))
		}

		if err == nil {
			return nil
		}

		// Handle checksum verification failure based on strict mode
		if i.options.StrictChecksum {
			// Strict mode: fail the installation when checksum verification fails
			return fmt.Errorf("checksum verification failed for %s: %w", filepath.Base(url), err)
		} else {
			// Non-strict mode: log warning and continue without verification
			t.Infof("⚠️ Checksum verification failed for %s: %v", filepath.Base(url), err)
		}
	}

	// Download without checksum verification (only reached in non-strict mode or when no checksum is configured)
	return download.Download(url, dest, t, download.WithCacheDir(i.options.CacheDir))
}
