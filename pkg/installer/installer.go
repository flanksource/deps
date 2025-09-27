package installer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
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
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
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
					task.Debugf("Resolving version constraint '%s' for %s", depConstraint, depName)
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

	// If no version specified, resolve it
	if version == "" {
		version = "latest"
	}

	t.Infof("Resolving version constraint '%s' for %s", version, name)

	// Resolve version constraint to specific version
	resolvedVersion, err := i.resolveVersionConstraint(ctx, mgr, pkg, version, t)
	if err != nil {
		return fmt.Errorf("failed to resolve version constraint for %s: %w", name, err)
	}

	t.Infof("Resolved %s version: %s -> %s", name, version, resolvedVersion)

	// Update task name to include the resolved version
	t.SetName(fmt.Sprintf("%s@%s", name, resolvedVersion))

	// Check if binary already exists with correct version (unless force flag is set)
	if !i.options.Force {
		if existingVersion := i.checkExistingVersion(name, pkg, resolvedVersion, t); existingVersion != "" {
			t.Infof("✅ %s@%s is already installed", name, resolvedVersion)
			t.Success()
			return nil
		}
	}

	// Create platform info (now using global overrides set from CLI)
	plat := platform.Current()

	t.Debugf("Install: using platform OS=%s, Arch=%s for %s", plat.OS, plat.Arch, name)

	// Resolve the package
	t.Infof("Fetching download URL for %s@%s using %s", name, resolvedVersion, mgr.Name())
	resolution, err := mgr.Resolve(ctx, pkg, resolvedVersion, plat)
	if err != nil {
		return fmt.Errorf("failed to resolve package %s: %w", name, err)
	}

	t.Debugf("Install: resolved package %s download URL: %s", name, resolution.DownloadURL)
	t.Infof("Downloading %s@%s from %s", name, resolvedVersion, resolution.DownloadURL)

	// Create bin directory if it doesn't exist
	absBinDir, _ := filepath.Abs(i.options.BinDir)
	if err := os.MkdirAll(i.options.BinDir, 0755); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}
	t.Debugf("Install: created/using bin directory %s", absBinDir)

	var downloadPath string
	finalPath := filepath.Join(i.options.BinDir, name)
	absFinalPath, _ := filepath.Abs(finalPath)

	if resolution.IsArchive {
		// Download to temp file with extension preserved
		ext := getArchiveExtension(resolution.DownloadURL)
		downloadPath = filepath.Join(i.options.TmpDir, fmt.Sprintf("deps-%s-%s%s", name, resolvedVersion, ext))
		absDownloadPath, _ := filepath.Abs(downloadPath)

		t.Debugf("Install: downloading archive %s to %s", name, absDownloadPath)

		// Download the archive with checksum verification if available
		if err := i.downloadWithChecksum(resolution.DownloadURL, downloadPath, resolution.ChecksumURL, resolution, t); err != nil {
			return fmt.Errorf("failed to download %s: %w", name, err)
		}

		// Auto-extract archive to working directory immediately after download
		// This ensures pipeline operations can work on extracted files
		workDir := filepath.Join(i.options.TmpDir, fmt.Sprintf("deps-extract-%s-%s", name, resolvedVersion))
		absWorkDir, _ := filepath.Abs(workDir)

		t.Infof("Auto-extracting %s archive", name)
		t.Debugf("Install: auto-extracting archive from %s to %s", absDownloadPath, absWorkDir)

		t.Debugf("DEBUG: Starting extraction for %s", name)
		extractResult, err := extract.Extract(downloadPath, workDir, t, extract.WithFullExtract())
		if err != nil {
			t.Errorf("DEBUG: Extraction failed for %s: %v", name, err)
			return fmt.Errorf("failed to extract archive: %w", err)
		}
		t.Debugf("DEBUG: Extraction completed for %s, result: %v", name, extractResult)

		// List contents after archive extraction for debugging
		if entries, err := os.ReadDir(workDir); err == nil {
			var fileNames []string
			for _, entry := range entries {
				fileNames = append(fileNames, entry.Name())
			}
			t.Debugf("Install: extracted contents: %v", fileNames)
			t.Infof("DEBUG: Successfully extracted %d entries from %s", len(fileNames), name)
		} else {
			t.Errorf("DEBUG: Failed to read workDir %s after extraction: %v", workDir, err)
		}

		t.Infof("Archive auto-extraction completed for %s", name)

		// Check if package has post-processing pipeline
		if len(pkg.PostProcess) > 0 {
			t.Infof("Install: post-process pipeline: %v", pkg.PostProcess)

			// Create CEL pipeline from expressions
			celPipeline := pipeline.NewCELPipeline(pkg.PostProcess)
			if celPipeline == nil {
				return fmt.Errorf("failed to create CEL pipeline from expressions: %v", pkg.PostProcess)
			}

			// Set up cleanup for working directory
			defer func() {
				if !i.options.Debug && !i.shouldSkipCleanup() {
					os.RemoveAll(workDir)
					os.Remove(downloadPath)
				} else if i.shouldSkipCleanup() {
					t.Infof("Install: keeping temporary files (--tmp-dir specified): %s, %s", workDir, downloadPath)
				} else {
					t.Infof("Install: keeping temporary files for debugging: %s, %s", workDir, downloadPath)
				}
			}()

			// Execute the CEL pipeline on the extracted contents
			evaluator := pipeline.NewCELPipelineEvaluator(workDir, path.Join(i.options.BinDir, name), i.options.TmpDir, t, i.options.Debug)
			if err := evaluator.Execute(celPipeline); err != nil {
				return err
			}

			t.Infof("Install: post-process pipeline completed successfully")

			// Make executable after post-processing completes
			if fileInfo, err := os.Stat(finalPath); err == nil && !fileInfo.IsDir() {
				if err := os.Chmod(finalPath, 0755); err != nil {
					return fmt.Errorf("failed to make binary executable: %w", err)
				}
				t.Debugf("Install: made %s executable", absFinalPath)
			} else if fileInfo != nil && fileInfo.IsDir() {
				t.Debugf("Install: skipping chmod for directory %s", absFinalPath)
			}

		} else {
			// Regular binary search in already extracted archive
			defer func() {
				if !i.options.Debug && !i.shouldSkipCleanup() {
					os.RemoveAll(workDir)
					os.Remove(downloadPath)
				} else if i.shouldSkipCleanup() {
					t.Infof("Install: keeping temporary files (--tmp-dir specified): %s, %s", workDir, downloadPath)
				}
			}()

			t.Debugf("Install: searching for binary in %s (binaryPath=%s)", absWorkDir, resolution.BinaryPath)

			binaryPath, err := extract.FindBinaryInDir(workDir, resolution.BinaryPath, t)
			if err != nil {
				return fmt.Errorf("failed to find binary %s: %w", name, err)
			}

			absBinaryPath, _ := filepath.Abs(binaryPath)
			t.Debugf("Install: found binary at %s", absBinaryPath)

			// Copy binary to final destination
			t.Infof("Installing %s binary", name)
			t.Debugf("Install: copying binary from %s to %s", absBinaryPath, absFinalPath)

			if err := copyFile(binaryPath, finalPath); err != nil {
				return fmt.Errorf("failed to install binary: %w", err)
			}
		}
	} else {
		// Direct binary download
		t.Debugf("Install: downloading direct binary %s to %s", name, absFinalPath)

		if err := i.downloadWithChecksum(resolution.DownloadURL, finalPath, resolution.ChecksumURL, resolution, t); err != nil {
			return fmt.Errorf("failed to download %s: %w", name, err)
		}
	}

	// Make executable (skip for directories and when post-process was used)
	if len(pkg.PostProcess) == 0 {
		fileInfo, err := os.Stat(finalPath)
		if err == nil && !fileInfo.IsDir() {
			if err := os.Chmod(finalPath, 0755); err != nil {
				return fmt.Errorf("failed to make binary executable: %w", err)
			}
			t.Debugf("Install: made %s executable", absFinalPath)
		} else if fileInfo != nil && fileInfo.IsDir() {
			t.Debugf("Install: skipping chmod for directory %s", absFinalPath)
		}

	}

	// Mark task successful only after all operations (including post-processing) complete
	t.Infof("Successfully installed %s@%s to %s", name, resolvedVersion, absFinalPath)
	t.Success()

	return nil
}

// resolveVersionConstraint resolves a version constraint to a specific version
func (i *Installer) resolveVersionConstraint(ctx context.Context, mgr manager.PackageManager, pkg types.Package, constraint string, t *task.Task) (string, error) {
	// Use the centralized version resolver
	resolver := version.NewResolver(mgr)
	return resolver.ResolveConstraint(ctx, pkg, constraint, platform.Current())
}

// getArchiveExtension returns the archive extension from a URL
func getArchiveExtension(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(lower, ".tgz"):
		return ".tgz"
	case strings.HasSuffix(lower, ".zip"):
		return ".zip"
	case strings.HasSuffix(lower, ".tar.xz"):
		return ".tar.xz"
	case strings.HasSuffix(lower, ".tar.bz2"):
		return ".tar.bz2"
	case strings.HasSuffix(lower, ".tar"):
		return ".tar"
	case strings.HasSuffix(lower, ".jar"):
		return ".jar"
	case strings.HasSuffix(lower, ".txz"):
		return ".txz"
	default:
		return ""
	}
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// downloadWithChecksum attempts to download with checksum verification
// using the provided checksum URL first, then falling back to URL pattern detection
func (i *Installer) downloadWithChecksum(url, dest, checksumURL string, resolution *types.Resolution, t *task.Task) error {
	// Skip checksum verification if requested
	if i.options.SkipChecksum {
		t.Debugf("Skipping checksum verification (--skip-checksum)")
		return download.Download(url, dest, t)
	}

	// Try the provided checksum URL if configured
	if checksumURL != "" {
		t.Debugf("Using configured checksum URL: %s", checksumURL)

		// Split comma-separated checksum URLs
		checksumURLs := strings.Split(checksumURL, ",")
		for i, url := range checksumURLs {
			checksumURLs[i] = strings.TrimSpace(url)
		}

		// Get checksum expression from package configuration
		checksumExpr := resolution.Package.ChecksumExpr

		var err error
		if len(checksumURLs) > 1 || checksumExpr != "" {
			// Use multi-file checksum with CEL support
			err = download.Download(url, dest, t, download.WithChecksumURLs(checksumURLs, checksumExpr))
		} else {
			// Use single checksum file
			err = download.Download(url, dest, t, download.WithChecksumURL(checksumURL))
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
	return download.Download(url, dest, t)
}

// checkExistingVersion checks if a binary exists and matches the requested version
// Returns the existing version if it matches, empty string otherwise
func (i *Installer) checkExistingVersion(name string, pkg types.Package, requestedVersion string, t *task.Task) string {
	// Determine binary name - handle special cases
	binaryName := name
	if pkg.BinaryName != "" {
		binaryName = pkg.BinaryName
	}

	// For Windows, add .exe extension if not present
	if filepath.Ext(binaryName) == "" && (i.options.OSOverride == "windows" ||
		(i.options.OSOverride == "" && platform.Current().OS == "windows")) {
		binaryName += ".exe"
	}

	binaryPath := filepath.Join(i.options.BinDir, binaryName)

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Debugf("Binary not found at %s", binaryPath)
		return ""
	}

	t.Infof("Found existing %s, checking version...", name)

	// Try to get the installed version
	installedVersion, err := version.GetInstalledVersion(binaryPath, pkg.VersionCommand, pkg.VersionPattern)
	if err != nil {
		t.Debugf("Could not verify existing version: %v", err)
		return ""
	}

	// Normalize both versions for comparison
	normalizedInstalled := version.Normalize(installedVersion)
	normalizedRequested := version.Normalize(requestedVersion)

	if normalizedInstalled == normalizedRequested {
		t.Debugf("Existing version %s matches requested %s", installedVersion, requestedVersion)
		return installedVersion
	}

	t.Infof("Existing version %s doesn't match requested %s, updating...", installedVersion, requestedVersion)
	return ""
}

// isArchive returns true if the file appears to be an archive based on its extension
func isArchive(path string) bool {
	lower := strings.ToLower(path)
	extensions := []string{".tar", ".tar.gz", ".tgz", ".tar.xz", ".txz", ".zip", ".jar"}
	for _, ext := range extensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
