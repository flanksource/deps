package lock

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/deps/pkg/checksum"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
	log "github.com/sirupsen/logrus"
)

// Generator generates lock files with multi-platform support
type Generator struct {
	managers  *manager.Registry
	discovery *checksum.Discovery
}

// NewGenerator creates a new lock file generator
func NewGenerator(managers *manager.Registry) *Generator {
	return &Generator{
		managers:  managers,
		discovery: checksum.NewDiscovery(),
	}
}

// Generate creates a lock file from dependencies and options
func (g *Generator) Generate(ctx context.Context, deps map[string]string, registry map[string]types.Package, opts types.LockOptions, mainTask *task.Task) (*types.LockFile, error) {
	lockFile := &types.LockFile{
		Version:         "1.0",
		Generated:       time.Now(),
		CurrentPlatform: platform.Current(),
		Dependencies:    make(map[string]types.LockEntry),
	}

	// Mutex to protect concurrent access to lockFile
	var lockFileMutex sync.Mutex

	platforms := g.getPlatformsToLock(opts)

	// Filter dependencies if specific packages are requested
	filteredDeps := g.filterDependencies(deps, opts.Packages)

	mainTask.Infof("Locking %d dependencies for %d platforms", len(filteredDeps), len(platforms))

	// Start individual tasks for each dependency-platform combination
	for name, versionConstraint := range filteredDeps {
		pkg, exists := registry[name]
		if !exists {
			mainTask.Errorf("Package %s not found in registry", name)
			continue
		}

		// Initialize entry for this dependency
		lockFile.Dependencies[name] = types.LockEntry{
			Version:        "",
			VersionCommand: pkg.VersionCommand,
			VersionPattern: pkg.VersionPattern,
			Platforms:      make(map[string]types.PlatformEntry),
		}

		// Create tasks for each platform
		for _, platform := range platforms {
			taskName := fmt.Sprintf("%s-%s", name, platform.String())
			platformStr := platform.String()

			task.StartTask(taskName, func(ctx flanksourceContext.Context, t *task.Task) (interface{}, error) {
				t.Infof("Resolving %s %s for %s...", name, versionConstraint, platformStr)

				entry, err := g.resolveDependencyForPlatform(ctx.Context, pkg, versionConstraint, platform, opts, t)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve %s for %s: %w", name, platformStr, err)
				}

				// Add success message with resolved version
				if versionConstraint != entry.Version {
					t.Infof("Resolved %s %s -> %s for %s", name, versionConstraint, entry.Version, platformStr)
				} else {
					t.Infof("Resolved %s %s for %s", name, entry.Version, platformStr)
				}

				// Update the lock file entry with proper synchronization
				lockFileMutex.Lock()
				if existingEntry, exists := lockFile.Dependencies[name]; exists {
					if existingEntry.Version == "" {
						existingEntry.Version = entry.Version
						existingEntry.GitHub = entry.GitHub
					}
					existingEntry.Platforms[platformStr] = entry.Platforms[platformStr]
					lockFile.Dependencies[name] = existingEntry
				}
				lockFileMutex.Unlock()

				return entry, nil
			})
		}
	}

	return lockFile, nil
}

// Update updates an existing lock file with new/changed dependencies
func (g *Generator) Update(ctx context.Context, existingLock *types.LockFile, deps map[string]string, registry map[string]types.Package, opts types.LockOptions, mainTask *task.Task) (*types.LockFile, error) {
	// Start with the existing lock file and preserve all dependencies
	mergedLock := &types.LockFile{
		Version:         "1.0",
		Generated:       time.Now(),
		CurrentPlatform: existingLock.CurrentPlatform,
		Dependencies:    make(map[string]types.LockEntry),
	}

	// Mutex to protect concurrent access to mergedLock
	var lockFileMutex sync.Mutex

	// Copy ALL existing dependencies first
	for name, entry := range existingLock.Dependencies {
		mergedLock.Dependencies[name] = entry
	}

	platforms := g.getPlatformsFromLock(existingLock, opts)

	// Filter dependencies if specific packages are requested
	filteredDeps := g.filterDependencies(deps, opts.Packages)

	mainTask.Infof("Updating %d dependencies for %d platforms", len(filteredDeps), len(platforms))

	// Process only dependencies that are in the current deps.yaml and match filter
	for name, versionConstraint := range filteredDeps {
		existingEntry, exists := mergedLock.Dependencies[name]

		if exists && versionConstraint == existingEntry.Version {
			// Check if we should skip this exact version dependency
			// Only skip if all requested platforms already exist in the lock file
			if version.LooksLikeExactVersion(versionConstraint) && !opts.Force {
				allPlatformsExist := true
				for _, plat := range platforms {
					platformStr := plat.String()
					if _, exists := existingEntry.Platforms[platformStr]; !exists {
						allPlatformsExist = false
						break
					}
				}

				if allPlatformsExist {
					mainTask.Infof("Skipping %s %s (already locked for all requested platforms, use --force to update)", name, versionConstraint)
					continue
				}
			}

			// Version hasn't changed, but we might need to update platforms
			pkg, pkgExists := registry[name]
			if !pkgExists {
				err := fmt.Errorf("package %s not found in registry", name)
				mainTask.Errorf("Failed to update platforms for %s: %v", name, err)
				continue
			}

			mainTask.Infof("Updating platforms for %s %s", name, versionConstraint)

			// Create tasks for each platform that needs updating
			for _, platform := range platforms {
				platformStr := platform.String()

				// Skip if platform already exists and we're not forcing
				if _, exists := existingEntry.Platforms[platformStr]; exists && !opts.Force {
					continue
				}

				taskName := fmt.Sprintf("%s-%s", name, platformStr)

				task.StartTask(taskName, func(ctx flanksourceContext.Context, t *task.Task) (interface{}, error) {
					t.Infof("Updating %s %s for %s...", name, versionConstraint, platformStr)

					entry, err := g.resolveDependencyForPlatform(ctx.Context, pkg, versionConstraint, platform, opts, t)
					if err != nil {
						return nil, fmt.Errorf("failed to update %s for %s: %w", name, platformStr, err)
					}

					t.Infof("Updated %s %s for %s", name, entry.Version, platformStr)

					// Update the platform entry with proper synchronization
					lockFileMutex.Lock()
					if existingEntry, exists := mergedLock.Dependencies[name]; exists {
						existingEntry.Platforms[platformStr] = entry.Platforms[platformStr]
						mergedLock.Dependencies[name] = existingEntry
					}
					lockFileMutex.Unlock()

					return entry, nil
				})
			}
			continue
		}

		// Version changed or new dependency - resolve it
		pkg, pkgExists := registry[name]
		if !pkgExists {
			mainTask.Errorf("Package %s not found in registry", name)
			continue
		}

		if exists {
			mainTask.Infof("Updating %s from %s to %s", name, existingEntry.Version, versionConstraint)
		} else {
			mainTask.Infof("Adding new dependency %s %s", name, versionConstraint)
		}

		// Initialize or preserve the entry
		if !exists {
			mergedLock.Dependencies[name] = types.LockEntry{
				Version:        "",
				VersionCommand: pkg.VersionCommand,
				VersionPattern: pkg.VersionPattern,
				Platforms:      make(map[string]types.PlatformEntry),
			}
		}

		// Create tasks for each platform
		for _, platform := range platforms {
			taskName := fmt.Sprintf("%s-%s", name, platform.String())
			platformStr := platform.String()

			task.StartTask(taskName, func(ctx flanksourceContext.Context, t *task.Task) (interface{}, error) {
				t.Infof("Resolving %s %s for %s...", name, versionConstraint, platformStr)

				entry, err := g.resolveDependencyForPlatform(ctx.Context, pkg, versionConstraint, platform, opts, t)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve %s for %s: %w", name, platformStr, err)
				}

				// Add success message with resolved version
				if versionConstraint != entry.Version {
					t.Infof("Resolved %s %s -> %s for %s", name, versionConstraint, entry.Version, platformStr)
				} else {
					t.Infof("Resolved %s %s for %s", name, entry.Version, platformStr)
				}

				// Update the lock file entry with proper synchronization
				lockFileMutex.Lock()
				if existingEntry, exists := mergedLock.Dependencies[name]; exists {
					if existingEntry.Version == "" {
						existingEntry.Version = entry.Version
						existingEntry.GitHub = entry.GitHub
					}
					existingEntry.Platforms[platformStr] = entry.Platforms[platformStr]
					mergedLock.Dependencies[name] = existingEntry
				}
				lockFileMutex.Unlock()

				return entry, nil
			})
		}
	}

	return mergedLock, nil
}

// resolveDependency resolves a single dependency for all specified platforms
func (g *Generator) resolveDependency(ctx context.Context, pkg types.Package, versionConstraint string, platforms []platform.Platform, opts types.LockOptions, existingEntry *types.LockEntry, t *task.Task) (*types.LockEntry, error) {
	// Get the package manager
	mgr, err := g.managers.GetForPackage(pkg)
	if err != nil {
		return nil, err
	}

	// Resolve version constraint
	version, err := g.resolveVersionConstraint(ctx, mgr, pkg, versionConstraint)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve version constraint %s for %s: %w", versionConstraint, formatPackageIdentifier(pkg), err)
	}

	entry := &types.LockEntry{
		Version:        version,
		VersionCommand: pkg.VersionCommand,
		VersionPattern: pkg.VersionPattern,
		Platforms:      make(map[string]types.PlatformEntry),
	}

	// Preserve existing platforms if they exist
	if existingEntry != nil {
		for platformStr, platformEntry := range existingEntry.Platforms {
			entry.Platforms[platformStr] = platformEntry
		}
		// Also preserve GitHub info if it exists
		if existingEntry.GitHub != nil {
			entry.GitHub = existingEntry.GitHub
		}
	}

	// Add GitHub-specific info if applicable
	if mgr.Name() == "github_release" {
		entry.GitHub = &types.GitHubLockInfo{
			Repo:         pkg.Repo,
			Tag:          version, // This might need normalization
			ChecksumFile: pkg.ChecksumFile,
		}
	}

	var platformSuccessCount int
	if opts.Parallel {
		platformSuccessCount, err = g.resolvePlatformsParallel(ctx, mgr, pkg, version, platforms, entry, opts, t)
	} else {
		platformSuccessCount, err = g.resolvePlatformsSequential(ctx, mgr, pkg, version, platforms, entry, opts, t)
	}

	if err != nil {
		return nil, err
	}

	// If no platforms were successfully resolved, treat it as a failure
	if platformSuccessCount == 0 && len(platforms) > 0 {
		return nil, fmt.Errorf("failed to resolve %s for any of the %d requested platforms", pkg.Name, len(platforms))
	}

	return entry, nil
}

// resolveDependencyForPlatform resolves a single dependency for a single platform
func (g *Generator) resolveDependencyForPlatform(ctx context.Context, pkg types.Package, versionConstraint string, plat platform.Platform, opts types.LockOptions, t *task.Task) (*types.LockEntry, error) {
	// Get the package manager
	mgr, err := g.managers.GetForPackage(pkg)
	if err != nil {
		return nil, err
	}

	// Resolve version constraint
	version, err := g.resolveVersionConstraint(ctx, mgr, pkg, versionConstraint)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve version constraint %s for %s: %w", versionConstraint, formatPackageIdentifier(pkg), err)
	}

	entry := &types.LockEntry{
		Version:        version,
		VersionCommand: pkg.VersionCommand,
		VersionPattern: pkg.VersionPattern,
		Platforms:      make(map[string]types.PlatformEntry),
	}

	// Add GitHub-specific info if applicable
	if mgr.Name() == "github_release" {
		entry.GitHub = &types.GitHubLockInfo{
			Repo:         pkg.Repo,
			Tag:          version,
			ChecksumFile: pkg.ChecksumFile,
		}
	}

	// Resolve this single platform
	platformEntry, err := g.resolvePlatform(ctx, mgr, pkg, version, plat, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s for %s: %w", pkg.Name, plat, err)
	}

	entry.Platforms[plat.String()] = *platformEntry
	return entry, nil
}

// resolvePlatformsSequential resolves platforms one by one
func (g *Generator) resolvePlatformsSequential(ctx context.Context, mgr manager.PackageManager, pkg types.Package, version string, platforms []platform.Platform, entry *types.LockEntry, opts types.LockOptions, t *task.Task) (int, error) {
	successCount := 0
	for _, plat := range platforms {
		platformEntry, err := g.resolvePlatform(ctx, mgr, pkg, version, plat, opts)
		if err != nil {
			t.Warnf("Failed to resolve %s for %s: %v", pkg.Name, plat, err)
			continue
		}

		// Update the platform entry (overwriting existing if present)
		entry.Platforms[plat.String()] = *platformEntry
		successCount++
	}
	return successCount, nil
}

// resolvePlatformsParallel resolves platforms concurrently
func (g *Generator) resolvePlatformsParallel(ctx context.Context, mgr manager.PackageManager, pkg types.Package, version string, platforms []platform.Platform, entry *types.LockEntry, opts types.LockOptions, t *task.Task) (int, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(map[string]types.PlatformEntry)

	for _, plat := range platforms {
		wg.Add(1)
		go func(p platform.Platform) {
			defer wg.Done()

			platformEntry, err := g.resolvePlatform(ctx, mgr, pkg, version, p, opts)
			if err != nil {
				t.Warnf("Failed to resolve %s for %s: %v", pkg.Name, p, err)
				return
			}

			mu.Lock()
			results[p.String()] = *platformEntry
			mu.Unlock()
		}(plat)
	}

	wg.Wait()

	// Update only the platforms we resolved (preserving existing ones)
	for platform, platformEntry := range results {
		entry.Platforms[platform] = platformEntry
	}

	return len(results), nil
}

// resolvePlatform resolves a single platform
func (g *Generator) resolvePlatform(ctx context.Context, mgr manager.PackageManager, pkg types.Package, version string, plat platform.Platform, opts types.LockOptions) (*types.PlatformEntry, error) {
	resolution, err := mgr.Resolve(ctx, pkg, version, plat)
	if err != nil {
		return nil, err
	}

	entry := &types.PlatformEntry{
		URL:        resolution.DownloadURL,
		Archive:    resolution.IsArchive,
		BinaryPath: resolution.BinaryPath,
	}

	// Get checksum - prioritize resolution.Checksum from manager (e.g., GitHub asset digest)
	if resolution.Checksum != "" {
		// Checksum already provided by manager (e.g., from GitHub asset digest)
		entry.Checksum = resolution.Checksum
		entry.Size = resolution.Size
	} else if !opts.VerifyOnly {
		// No checksum available from manager - try to discover or calculate it
		if resolution.ChecksumURL != "" {
			// Try to get checksum from checksum file
			checksums, err := g.discovery.FindChecksums(ctx, resolution)
			if err == nil {
				// Find our specific file's checksum
				if resolution.GitHubAsset != nil {
					entry.Checksum = checksums[resolution.GitHubAsset.AssetName]
				}
			}
		}

		// Fallback: download and calculate checksum
		if entry.Checksum == "" {
			log.Debugf("Calculating checksum for %s %s", pkg.Name, plat)
			checksum, size, err := checksum.CalculateFileChecksum(ctx, resolution.DownloadURL)
			if err != nil {
				return nil, fmt.Errorf("failed to calculate checksum for %s %s: %w", pkg.Name, plat, err)
			}
			entry.Checksum = checksum
			entry.Size = size
		}
	}

	return entry, nil
}

// getPlatformsToLock determines which platforms to lock based on options
func (g *Generator) getPlatformsToLock(opts types.LockOptions) []platform.Platform {
	var platforms []platform.Platform
	var source string

	if opts.All {
		platforms = platform.CommonPlatforms()
		source = "all common platforms"
	} else if len(opts.Platforms) > 0 {
		var err error
		platforms, err = platform.ParseList(opts.Platforms)
		if err != nil {
			log.Warnf("Failed to parse platforms: %v, using current platform", err)
			platforms = []platform.Platform{platform.Current()}
			source = "current platform (fallback)"
		} else {
			source = "specified platforms"
		}
	} else {
		// Default: current platform only
		platforms = []platform.Platform{platform.Current()}
		source = "current platform (default)"
	}

	platformNames := make([]string, len(platforms))
	for i, p := range platforms {
		platformNames[i] = p.String()
	}
	log.Infof("Using platforms from %s: %v", source, platformNames)

	return platforms
}

// getPlatformsFromLock extracts platforms from an existing lock file
func (g *Generator) getPlatformsFromLock(lockFile *types.LockFile, opts types.LockOptions) []platform.Platform {
	var platforms []platform.Platform
	var source string

	if opts.All {
		platforms = platform.CommonPlatforms()
		source = "all common platforms"
	} else if len(opts.Platforms) > 0 {
		var err error
		platforms, err = platform.ParseList(opts.Platforms)
		if err != nil {
			log.Warnf("Failed to parse platforms: %v, using current platform", err)
			platforms = []platform.Platform{platform.Current()}
			source = "current platform (fallback)"
		} else {
			source = "specified platforms"
		}
	} else {
		// Extract platforms from existing lock file
		platformMap := make(map[string]bool)
		for _, entry := range lockFile.Dependencies {
			for platformStr := range entry.Platforms {
				platformMap[platformStr] = true
			}
		}

		if len(platformMap) == 0 {
			platforms = []platform.Platform{platform.Current()}
			source = "current platform (no existing platforms)"
		} else {
			for platformStr := range platformMap {
				if plat, err := platform.Parse(platformStr); err == nil {
					platforms = append(platforms, plat)
				}
			}
			source = "existing lock file"
		}
	}

	platformNames := make([]string, len(platforms))
	for i, p := range platforms {
		platformNames[i] = p.String()
	}
	log.Infof("Using platforms from %s: %v", source, platformNames)

	return platforms
}

// resolveVersionConstraint resolves a semver constraint to a specific version
func (g *Generator) resolveVersionConstraint(ctx context.Context, mgr manager.PackageManager, pkg types.Package, constraint string) (string, error) {
	// Use the centralized version resolver
	resolver := version.NewResolver(mgr)
	return resolver.ResolveConstraint(ctx, pkg, constraint, platform.Current())
}

// formatPackageIdentifier creates a consistent package identifier string with relevant details
func formatPackageIdentifier(pkg types.Package) string {
	if pkg.Manager == "github_release" && pkg.Repo != "" {
		return fmt.Sprintf("%s (repo: %s)", pkg.Name, pkg.Repo)
	}
	return fmt.Sprintf("%s (%s)", pkg.Name, pkg.Manager)
}

// filterDependencies filters dependencies based on package names
func (g *Generator) filterDependencies(deps map[string]string, packages []string) map[string]string {
	// If no specific packages requested, return all dependencies
	if len(packages) == 0 {
		return deps
	}

	// Create a map for fast lookup
	packageSet := make(map[string]bool)
	for _, pkg := range packages {
		packageSet[pkg] = true
	}

	// Filter dependencies
	filtered := make(map[string]string)
	for name, constraint := range deps {
		if packageSet[name] {
			filtered[name] = constraint
		}
	}

	return filtered
}
