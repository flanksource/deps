package manager

import (
	"context"
	"strings"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

// Context keys for passing options to managers
type contextKey string

const (
	// StrictChecksumKey is used to pass strict checksum mode to managers
	StrictChecksumKey contextKey = "strictChecksum"
	// IterateVersionsKey is used to pass the max number of releases to try when assets not found
	IterateVersionsKey contextKey = "iterateVersions"
)

// WithStrictChecksum returns a context with strict checksum mode set
func WithStrictChecksum(ctx context.Context, strict bool) context.Context {
	return context.WithValue(ctx, StrictChecksumKey, strict)
}

// GetStrictChecksum returns the strict checksum mode from context
func GetStrictChecksum(ctx context.Context) bool {
	if v := ctx.Value(StrictChecksumKey); v != nil {
		if strict, ok := v.(bool); ok {
			return strict
		}
	}
	return true // Default to strict mode
}

// WithIterateVersions returns a context with iterate-versions setting
func WithIterateVersions(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, IterateVersionsKey, n)
}

// GetIterateVersions returns the max number of releases to try from context
// Returns 0 if not set (iteration disabled)
func GetIterateVersions(ctx context.Context) int {
	if v := ctx.Value(IterateVersionsKey); v != nil {
		if n, ok := v.(int); ok {
			return n
		}
	}
	return 0
}

// PackageManager defines the interface for different package managers
type PackageManager interface {
	// Name returns the manager type identifier
	Name() string

	// DiscoverVersions returns the most recent versions for a package
	// limit=0 means return all versions, limit>0 means return at most that many
	// Results should be ordered with newest versions first
	DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error)

	// Resolve gets the download URL and checksum for a specific version and platform
	Resolve(ctx context.Context, pkg types.Package, version string, platform platform.Platform) (*types.Resolution, error)

	// Install downloads and installs a binary for the given resolution
	Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error

	// GetChecksums retrieves checksums for all platforms for a given version
	GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error)

	// Verify checks if an installed binary matches the expected version/checksum
	Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error)
}

// Registry holds all registered package managers
type Registry struct {
	managers map[string]PackageManager
}

// NewRegistry creates a new package manager registry
func NewRegistry() *Registry {
	return &Registry{
		managers: make(map[string]PackageManager),
	}
}

// Register adds a package manager to the registry
func (r *Registry) Register(manager PackageManager) {
	r.managers[manager.Name()] = manager
}

// Get retrieves a package manager by name
func (r *Registry) Get(name string) (PackageManager, bool) {
	manager, exists := r.managers[name]
	return manager, exists
}

// List returns all registered package manager names
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.managers))
	for name := range r.managers {
		names = append(names, name)
	}
	return names
}

// GetForPackage returns the appropriate manager for a package
func (r *Registry) GetForPackage(pkg types.Package) (PackageManager, error) {
	manager, exists := r.Get(pkg.Manager)
	if !exists {
		return nil, &ErrManagerNotFound{Manager: pkg.Manager}
	}
	return manager, nil
}

// Errors

// ErrManagerNotFound is returned when a package manager is not found
type ErrManagerNotFound struct {
	Manager string
}

func (e *ErrManagerNotFound) Error() string {
	return "package manager not found: " + e.Manager
}

// ErrVersionNotFound is returned when a version is not found
type ErrVersionNotFound struct {
	Package string
	Version string
}

func (e *ErrVersionNotFound) Error() string {
	return e.Version + " not found"
}

// ErrPlatformNotSupported is returned when a platform is not supported
type ErrPlatformNotSupported struct {
	Package            string
	Platform           string
	AvailablePlatforms []string
}

func (e *ErrPlatformNotSupported) Error() string {
	msg := "platform " + e.Platform + " not supported"
	if e.Package != "" {
		msg += " for " + e.Package
	}
	if len(e.AvailablePlatforms) > 0 {
		msg += ", available platforms: " + strings.Join(e.AvailablePlatforms, ", ")
	}
	return msg
}

// ErrChecksumMismatch is returned when checksums don't match
type ErrChecksumMismatch struct {
	Expected string
	Actual   string
	File     string
}

func (e *ErrChecksumMismatch) Error() string {
	return "checksum mismatch for " + e.File + ": expected " + e.Expected + ", got " + e.Actual
}

// ErrAssetNotFound is returned when a specific asset is not found in a release
type ErrAssetNotFound struct {
	Package         string
	AssetPattern    string
	Platform        string
	AvailableAssets []string
}

func (e *ErrAssetNotFound) Error() string {
	return "asset not found: " + e.AssetPattern + " for " + e.Platform + " in package " + e.Package
}

// Global package manager registry
var globalRegistry = NewRegistry()

// Register adds a package manager to the global registry
func Register(manager PackageManager) {
	globalRegistry.Register(manager)
}

// GetGlobalRegistry returns the global package manager registry
func GetGlobalRegistry() *Registry {
	return globalRegistry
}
