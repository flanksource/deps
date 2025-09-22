package types

import (
	"time"

	"github.com/flanksource/deps/pkg/platform"
)

// VersionCheckMode defines how version checking should be performed
type VersionCheckMode int

const (
	// VersionCheckNone skips version checking entirely
	VersionCheckNone VersionCheckMode = iota
	// VersionCheckExact requires an exact version match
	VersionCheckExact
	// VersionCheckMinimum requires the installed version to be at least the specified version
	VersionCheckMinimum
	// VersionCheckCompatible allows compatible versions (same major version)
	VersionCheckCompatible
)

// Package represents a package definition from the registry
type Package struct {
	Name           string                 `json:"name" yaml:"name"`
	Manager        string                 `json:"manager" yaml:"manager"`
	Repo           string                 `json:"repo,omitempty" yaml:"repo,omitempty"`                       // For GitHub: "owner/repo"
	URLTemplate    string                 `json:"url_template,omitempty" yaml:"url_template,omitempty"`       // For direct URLs
	AssetPatterns  map[string]string      `json:"asset_patterns,omitempty" yaml:"asset_patterns,omitempty"`   // Platform -> asset pattern
	ChecksumFile   string                 `json:"checksum_file,omitempty" yaml:"checksum_file,omitempty"`     // Checksum file pattern (comma-separated for multiple files)
	ChecksumExpr   string                 `json:"checksum_expr,omitempty" yaml:"checksum_expr,omitempty"`     // CEL expression to extract checksum from file contents
	VersionCommand string                 `json:"version_command,omitempty" yaml:"version_command,omitempty"` // Command to get version
	VersionPattern string                 `json:"version_pattern,omitempty" yaml:"version_pattern,omitempty"` // Regex to extract version
	BinaryName     string                 `json:"binary_name,omitempty" yaml:"binary_name,omitempty"`         // Custom binary name
	BinaryPath     string                 `json:"binary_path,omitempty" yaml:"binary_path,omitempty"`         // Path within archive (supports CEL expressions)
	PreInstalled   []string               `json:"pre_installed,omitempty" yaml:"pre_installed,omitempty"`     // Pre-installed binary names
	PostProcess    string                 `json:"post_process,omitempty" yaml:"post_process,omitempty"`       // Pipeline operations after download (e.g., "unarchive(glob('*.txz')) && chdir(glob('*:dir'))")
	Extra          map[string]interface{} `json:"extra,omitempty" yaml:"extra,omitempty"`                     // Manager-specific config
}

// Dependency represents a dependency specification with version constraints
type Dependency struct {
	Package string `json:"package" yaml:"package"`
	Version string `json:"version" yaml:"version"` // Semver constraint
}

// Resolution represents a resolved download for a specific version and platform
type Resolution struct {
	Package     Package           `json:"package" yaml:"package"`
	Version     string            `json:"version" yaml:"version"`
	Platform    platform.Platform `json:"platform" yaml:"platform"`
	DownloadURL string            `json:"download_url" yaml:"download_url"`
	ChecksumURL string            `json:"checksum_url,omitempty" yaml:"checksum_url,omitempty"`
	Checksum    string            `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	Size        int64             `json:"size,omitempty" yaml:"size,omitempty"`
	IsArchive   bool              `json:"is_archive" yaml:"is_archive"`
	BinaryPath  string            `json:"binary_path,omitempty" yaml:"binary_path,omitempty"` // Path within archive
	GitHubAsset *GitHubAsset      `json:"github_asset,omitempty" yaml:"github_asset,omitempty"`
}

// GitHubAsset contains GitHub-specific metadata
type GitHubAsset struct {
	Repo        string `json:"repo" yaml:"repo"`
	Tag         string `json:"tag" yaml:"tag"`
	AssetName   string `json:"asset_name" yaml:"asset_name"`
	AssetID     int64  `json:"asset_id,omitempty" yaml:"asset_id,omitempty"`
	DownloadURL string `json:"download_url" yaml:"download_url"`
}

// PlatformEntry represents a platform-specific entry in the lock file
type PlatformEntry struct {
	URL        string `json:"url" yaml:"url"`
	Checksum   string `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	Size       int64  `json:"size,omitempty" yaml:"size,omitempty"`
	Archive    bool   `json:"archive,omitempty" yaml:"archive,omitempty"`
	BinaryPath string `json:"binary_path,omitempty" yaml:"binary_path,omitempty"`
}

// LockEntry represents a single dependency in the lock file
type LockEntry struct {
	Version        string                   `json:"version" yaml:"version"`
	VersionCommand string                   `json:"version_command,omitempty" yaml:"version_command,omitempty"`
	VersionPattern string                   `json:"version_pattern,omitempty" yaml:"version_pattern,omitempty"`
	Platforms      map[string]PlatformEntry `json:"platforms" yaml:"platforms"`
	GitHub         *GitHubLockInfo          `json:"github,omitempty" yaml:"github,omitempty"`
}

// GitHubLockInfo contains GitHub-specific information in the lock file
type GitHubLockInfo struct {
	Repo         string `json:"repo" yaml:"repo"`
	Tag          string `json:"tag" yaml:"tag"`
	ChecksumFile string `json:"checksum_file,omitempty" yaml:"checksum_file,omitempty"`
}

// LockFile represents the complete deps-lock.yaml structure
type LockFile struct {
	Version         string               `json:"version" yaml:"version"`
	Dependencies    map[string]LockEntry `json:"dependencies" yaml:"dependencies"`
	Generated       time.Time            `json:"generated" yaml:"generated"`
	CurrentPlatform platform.Platform    `json:"current_platform" yaml:"current_platform"`
}

// DepsConfig represents the deps.yaml configuration file
type DepsConfig struct {
	Dependencies map[string]string  `json:"dependencies" yaml:"dependencies"` // name -> version constraint
	Registry     map[string]Package `json:"registry" yaml:"registry"`         // name -> package definition
	Settings     Settings           `json:"settings" yaml:"settings"`
}

// Settings represents global configuration settings
type Settings struct {
	BinDir     string            `json:"bin_dir" yaml:"bin_dir"`
	CacheDir   string            `json:"cache_dir,omitempty" yaml:"cache_dir,omitempty"`
	Platform   platform.Platform `json:"platform" yaml:"platform"`
	Parallel   bool              `json:"parallel,omitempty" yaml:"parallel,omitempty"`
	SkipVerify bool              `json:"skip_verify,omitempty" yaml:"skip_verify,omitempty"`
}

// InstallOptions configures installation behavior
type InstallOptions struct {
	BinDir       string
	Platform     platform.Platform
	Force        bool
	SkipChecksum bool
	SkipVerify   bool
	Progress     bool
	Parallel     bool
	OutputDir    string // For cross-platform installation
}

// LockOptions configures lock file generation
type LockOptions struct {
	All        bool     // Lock all platforms
	Platforms  []string // Specific platforms to lock
	Packages   []string // Specific packages to lock (empty means all)
	Parallel   bool     // Download checksums in parallel
	VerifyOnly bool     // Only verify, don't download
	UpdateOnly bool     // Only update existing entries
	Force      bool     // Force re-resolution of all dependencies, even exact versions
}

// Version represents a discoverable version
type Version struct {
	Version    string    `json:"version" yaml:"version"`
	Tag        string    `json:"tag,omitempty" yaml:"tag,omitempty"`
	SHA        string    `json:"sha,omitempty" yaml:"sha,omitempty"`
	Published  time.Time `json:"published,omitempty" yaml:"published,omitempty"`
	Prerelease bool      `json:"prerelease,omitempty" yaml:"prerelease,omitempty"`
}

// InstalledInfo represents information about an installed binary
type InstalledInfo struct {
	Version  string            `json:"version" yaml:"version"`
	Path     string            `json:"path" yaml:"path"`
	Platform platform.Platform `json:"platform" yaml:"platform"`
	Checksum string            `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	Size     int64             `json:"size,omitempty" yaml:"size,omitempty"`
	ModTime  time.Time         `json:"mod_time" yaml:"mod_time"`
}
