package types

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/utils"
	"github.com/flanksource/gomplate/v3"
	"github.com/samber/lo"
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
	// Name is the package identifier used in dependencies
	Name string `json:"name" yaml:"name"`
	// Manager specifies which package manager to use (github_release, url, golang, maven, apache)
	Manager string `json:"manager" yaml:"manager"`
	// Repo is the repository identifier for GitHub packages (format: "owner/repo")
	Repo string `json:"repo,omitempty" yaml:"repo,omitempty"`
	// URLTemplate is a template string for direct download URLs with placeholders for {{.os}}, {{.arch}}, {{.version}}, etc.
	URLTemplate string `json:"url_template,omitempty" yaml:"url_template,omitempty"`
	// VersionsURL is the URL to fetch available versions (used by url manager)
	VersionsURL string `json:"versions_url,omitempty" yaml:"versions_url,omitempty"`
	// VersionsExpr is a CEL expression to extract structured version data from JSON response (returns array of {version, url?, checksum?, asset?})
	VersionsExpr string `json:"versions_expr,omitempty" yaml:"versions_expr,omitempty"`
	// AssetPatterns maps platform identifiers to asset name patterns for selecting the correct binary
	AssetPatterns map[string]string `json:"asset_patterns,omitempty" yaml:"asset_patterns,omitempty"`
	// ChecksumFile is the pattern for checksum file names (supports comma-separated list for multiple files)
	ChecksumFile string `json:"checksum_file,omitempty" yaml:"checksum_file,omitempty"`
	// ChecksumExpr is a CEL expression to extract checksums from file contents
	ChecksumExpr string `json:"checksum_expr,omitempty" yaml:"checksum_expr,omitempty"`
	// AssetsExpr is a CEL expression for JSON-based asset discovery (returns {url, checksum})
	AssetsExpr string `json:"assets_expr,omitempty" yaml:"assets_expr,omitempty"`
	// VersionCommand is the command to execute to get the installed version
	VersionCommand string `json:"version_command,omitempty" yaml:"version_command,omitempty"`
	// VersionRegex is a regular expression pattern to extract version from command output
	VersionRegex string `json:"version_regex,omitempty" yaml:"version_regex,omitempty"`
	// VersionExpr is a CEL expression to filter and transform discovered versions
	VersionExpr string `json:"version_expr,omitempty" yaml:"version_expr,omitempty"`
	// BinaryName specifies a custom name for the binary (defaults to package name)
	BinaryName string `json:"binary_name,omitempty" yaml:"binary_name,omitempty"`
	// BinaryPath is the path within an archive to the binary (supports CEL expressions)
	BinaryPath string `json:"binary_path,omitempty" yaml:"binary_path,omitempty"`
	// PreInstalled lists binary names that may already be installed on the system
	PreInstalled []string `json:"pre_installed,omitempty" yaml:"pre_installed,omitempty"`
	// Extract overrides auto-detection of archive extraction (nil=auto, true=force, false=skip)
	Extract *bool `json:"extract,omitempty" yaml:"extract,omitempty"`
	// PostProcess is a list of CEL pipeline operations to run after download (supports platform prefixes like "!windows*: rm(glob('*.bat'))")
	PostProcess []string `json:"post_process,omitempty" yaml:"post_process,omitempty"`
	// Mode specifies the installation mode: "binary" (default) or "directory"
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`
	// Symlinks contains glob patterns of paths in app-dir to symlink to bin-dir (directory mode only, supports platform prefixes)
	Symlinks []string `json:"symlinks,omitempty" yaml:"symlinks,omitempty"`
	// WrapperScript is a template for creating a wrapper script in bin-dir (supports {{.appDir}}, {{.binDir}}, {{.name}}, {{.version}}, {{.os}}, {{.arch}})
	WrapperScript string `json:"wrapper_script,omitempty" yaml:"wrapper_script,omitempty"`
	// Extra contains manager-specific configuration options
	Extra map[string]interface{} `json:"extra,omitempty" yaml:"extra,omitempty"`
	// FallbackVersion is used when GitHub API rate limits are reached (defaults to "latest")
	FallbackVersion string `json:"fallback_version,omitempty" yaml:"fallback_version,omitempty"`
}

func (p Package) TemplateURL(platform platform.Platform, v string) (string, error) {
	return p.Template(p.URLTemplate, platform, v)
}

func (p Package) Template(url string, platform platform.Platform, v string) (string, error) {
	if url == "" {
		return "", nil
	}
	data := map[string]any{
		"tag":     utils.Normalize(v),
		"name":    p.Name,
		"version": v,
		"os":      platform.OS,
		"arch":    platform.Arch,
	}

	v, err := gomplate.RunTemplate(data, gomplate.Template{Template: url})
	if err != nil {
		logger.Warnf("Failed to template URL %s: %v", url, err)
	}
	logger.V(3).Infof("Templated URL: %s from %v", v, data)
	return v, err
}

// Dependency represents a dependency specification with version constraints
type Dependency struct {
	// Package is the name of the package to install
	Package string `json:"package" yaml:"package"`
	// Version is the semver constraint or version requirement (e.g., "^1.2.0", "latest", "stable")
	Version string `json:"version" yaml:"version"`
}

// Resolution represents a resolved download for a specific version and platform
type Resolution struct {
	// Package is the package definition being resolved
	Package Package `json:"package" yaml:"package"`
	// Version is the specific version number that was resolved
	Version string `json:"version" yaml:"version"`
	// Platform identifies the target OS and architecture
	Platform platform.Platform `json:"platform" yaml:"platform"`
	// DownloadURL is the direct URL to download the package
	DownloadURL string `json:"download_url" yaml:"download_url"`
	// ChecksumURL is the URL to download checksum file (if separate from main download)
	ChecksumURL string `json:"checksum_url,omitempty" yaml:"checksum_url,omitempty"`
	// Checksum is the expected checksum value for verification
	Checksum string `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	// Size is the download file size in bytes
	Size int64 `json:"size,omitempty" yaml:"size,omitempty"`
	// IsArchive indicates whether the download is an archive that needs extraction
	IsArchive bool `json:"is_archive" yaml:"is_archive"`
	// BinaryPath is the path within an archive to extract the binary from
	BinaryPath string `json:"binary_path,omitempty" yaml:"binary_path,omitempty"`
	// GitHubAsset contains GitHub-specific metadata if this is a GitHub release
	GitHubAsset *GitHubAsset `json:"github_asset,omitempty" yaml:"github_asset,omitempty"`
}

func (r Resolution) Pretty() api.Text {
	text := clicky.Text("").Append(r.Package.Name, "bold")

	if r.Version != "" {
		text = text.Append("@" + r.Version)
	}

	if r.Platform.OS != "" || r.Platform.Arch != "" {
		text = text.Append(" (" + r.Platform.String() + ")")
	}

	if r.DownloadURL != "" {
		text = text.Append(" -> ", "text-muted").Append(r.DownloadURL, "text-underline")
	}

	if r.Checksum != "" {
		text = text.Append(" checksum:", "muted").Append(lo.Ellipsis(r.Checksum, 10))
	}
	if r.GitHubAsset != nil {
		text = text.Append(" from ", "text-muted").Append(r.GitHubAsset.Repo + "@" + r.GitHubAsset.Tag)
	}

	return text
}

// GitHubAsset contains GitHub-specific metadata
type GitHubAsset struct {
	// Repo is the GitHub repository in "owner/repo" format
	Repo string `json:"repo" yaml:"repo"`
	// Tag is the Git tag/release name
	Tag string `json:"tag" yaml:"tag"`
	// AssetName is the filename of the release asset
	AssetName string `json:"asset_name" yaml:"asset_name"`
	// AssetID is the GitHub asset ID number
	AssetID int64 `json:"asset_id,omitempty" yaml:"asset_id,omitempty"`
	// DownloadURL is the direct download URL for the asset
	DownloadURL string `json:"download_url" yaml:"download_url"`
}

// PlatformEntry represents a platform-specific entry in the lock file
type PlatformEntry struct {
	// URL is the download URL for this platform
	URL string `json:"url" yaml:"url"`
	// Checksum is the SHA256 checksum for verification
	Checksum string `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	// Size is the download file size in bytes
	Size int64 `json:"size,omitempty" yaml:"size,omitempty"`
	// Archive indicates whether the download is an archive requiring extraction
	Archive bool `json:"archive,omitempty" yaml:"archive,omitempty"`
	// BinaryPath is the path within the archive to the binary
	BinaryPath string `json:"binary_path,omitempty" yaml:"binary_path,omitempty"`
}

// LockEntry represents a single dependency in the lock file
type LockEntry struct {
	// Version is the locked version number
	Version string `json:"version" yaml:"version"`
	// VersionCommand is the command to run to get the installed version
	VersionCommand string `json:"version_command,omitempty" yaml:"version_command,omitempty"`
	// VersionRegex is the regex pattern to extract version from command output
	VersionRegex string `json:"version_regex,omitempty" yaml:"version_regex,omitempty"`
	// Platforms maps platform identifiers to their specific download information
	Platforms map[string]PlatformEntry `json:"platforms" yaml:"platforms"`
	// GitHub contains GitHub-specific lock information if this is a GitHub release
	GitHub *GitHubLockInfo `json:"github,omitempty" yaml:"github,omitempty"`
}

// GitHubLockInfo contains GitHub-specific information in the lock file
type GitHubLockInfo struct {
	// Repo is the GitHub repository in "owner/repo" format
	Repo string `json:"repo" yaml:"repo"`
	// Tag is the Git tag/release name that was locked
	Tag string `json:"tag" yaml:"tag"`
	// ChecksumFile is the name of the checksum file in the GitHub release
	ChecksumFile string `json:"checksum_file,omitempty" yaml:"checksum_file,omitempty"`
}

// LockFile represents the complete deps-lock.yaml structure
type LockFile struct {
	// Version is the lock file format version
	Version string `json:"version" yaml:"version"`
	// Dependencies maps package names to their locked versions and platform-specific downloads
	Dependencies map[string]LockEntry `json:"dependencies" yaml:"dependencies"`
	// Generated is the timestamp when this lock file was created
	Generated time.Time `json:"generated" yaml:"generated"`
	// CurrentPlatform is the platform used when generating the lock file
	CurrentPlatform platform.Platform `json:"current_platform" yaml:"current_platform"`
}

// DepsConfig represents the deps.yaml configuration file
type DepsConfig struct {
	// Dependencies maps package names to version constraints
	Dependencies map[string]string `json:"dependencies" yaml:"dependencies"`
	// Registry maps package names to their full package definitions
	Registry map[string]Package `json:"registry" yaml:"registry"`
	// Settings contains global configuration options
	Settings Settings `json:"settings" yaml:"settings"`
}

// Settings represents global configuration settings
type Settings struct {
	// BinDir is the directory where binary symlinks/wrappers are installed
	BinDir string `json:"bin_dir" yaml:"bin_dir"`
	// AppDir is the directory where directory-mode packages are installed
	AppDir string `json:"app_dir,omitempty" yaml:"app_dir,omitempty"`
	// CacheDir is the directory for caching downloads
	CacheDir string `json:"cache_dir,omitempty" yaml:"cache_dir,omitempty"`
	// Platform overrides the detected OS and architecture
	Platform platform.Platform `json:"platform" yaml:"platform"`
	// Parallel enables parallel downloads and installations
	Parallel bool `json:"parallel,omitempty" yaml:"parallel,omitempty"`
	// SkipVerify disables checksum verification (not recommended for production)
	SkipVerify bool `json:"skip_verify,omitempty" yaml:"skip_verify,omitempty"`
}

// InstallOptions configures installation behavior
type InstallOptions struct {
	// BinDir is the directory where binaries should be installed
	BinDir string
	// Platform specifies the target OS and architecture for installation
	Platform platform.Platform
	// Force reinstalls even if the package is already installed
	Force bool
	// SkipChecksum skips checksum verification during download
	SkipChecksum bool
	// SkipVerify skips version verification after installation
	SkipVerify bool
	// Progress enables progress reporting during installation
	Progress bool
	// Parallel enables parallel installation of multiple packages
	Parallel bool
	// OutputDir specifies an alternate output directory for cross-platform installations
	OutputDir string
}

// LockOptions configures lock file generation
type LockOptions struct {
	// All locks dependencies for all common platforms
	All bool
	// Platforms is a list of specific platforms to lock (e.g., ["linux-amd64", "darwin-arm64"])
	Platforms []string
	// Packages is a list of specific package names to lock (empty means all dependencies)
	Packages []string
	// Parallel enables parallel checksum downloads for faster locking
	Parallel bool
	// VerifyOnly verifies existing lock entries without downloading
	VerifyOnly bool
	// UpdateOnly updates existing lock entries without adding new ones
	UpdateOnly bool
	// Force re-resolves all dependencies, even those with exact version pins
	Force bool
}

// Version represents a discoverable version
type Version struct {
	// Version is the version string (after normalization and version_expr transformations)
	Version string `json:"version" yaml:"version"`
	// Tag is the original tag name from the source (e.g., Git tag, release name)
	Tag string `json:"tag,omitempty" yaml:"tag,omitempty"`
	// SHA is the commit SHA or content hash for this version
	SHA string `json:"sha,omitempty" yaml:"sha,omitempty"`
	// Published is the timestamp when this version was published/released
	Published time.Time `json:"published,omitempty" yaml:"published,omitempty"`
	// Prerelease indicates whether this is a prerelease version (alpha, beta, rc, etc.)
	Prerelease bool `json:"prerelease,omitempty" yaml:"prerelease,omitempty"`
	// Normalized is the fully normalized semver version string
	Normalized string `json:"normalized,omitempty" yaml:"normalized,omitempty"`
	// Major is the major version component (e.g., 17 in 17.0.8)
	Major int64 `json:"major,omitempty" yaml:"major,omitempty"`
	// Minor is the minor version component (e.g., 0 in 17.0.8)
	Minor int64 `json:"minor,omitempty" yaml:"minor,omitempty"`
	// Patch is the patch version component (e.g., 8 in 17.0.8)
	Patch int64 `json:"patch,omitempty" yaml:"patch,omitempty"`
}

type InstallStatus string

const (
	InstallStatusInstalled        InstallStatus = "installed"
	InstallStatusForcedInstalled  InstallStatus = "forced_installed"
	InstallStatusAlreadyInstalled InstallStatus = "already_installed"
	InstallStatusFailed           InstallStatus = "failed"
)

func (s InstallStatus) Pretty() api.Text {
	switch s {
	case InstallStatusInstalled:
		return clicky.Text("").Add(icons.Success).Append(" Installed", "text-green-500")
	case InstallStatusForcedInstalled:
		return clicky.Text("").Add(icons.InfoAlt).Append(" Forced Installed", "text-blue-500")
	case InstallStatusAlreadyInstalled:
		return clicky.Text("").Add(icons.Skip).Append(" Already Installed", "text-yellow-500")
	case InstallStatusFailed:
		return clicky.Text("").Add(icons.Error).Append(" Failed", "text-red-500")
	default:
		return clicky.Text(string(s))
	}
}

type VerifyStatus string

const (
	VerifyStatusChecksumMatch    VerifyStatus = "verified"
	VerifyStatusChecksumMismatch VerifyStatus = "checksum_mismatch"
	VerifyStatusSkipped          VerifyStatus = "skipped"
)

func (s VerifyStatus) Pretty() api.Text {
	switch s {
	case VerifyStatusChecksumMatch:
		return clicky.Text("").Add(icons.Success).Append(" Checksum Match", "text-green-500")
	case VerifyStatusChecksumMismatch:
		return clicky.Text("").Add(icons.Error).Append(" Checksum Mismatch", "text-red-500")
	case VerifyStatusSkipped:
		return clicky.Text("").Add(icons.Skip).Append(" Checksum Skipped", "text-yellow-500")
	default:
		return clicky.Text(string(s))
	}
}

type VersionStatus string

const (
	VersionStatusValid   VersionStatus = "up-to-date"
	VersionStatusInvalid VersionStatus = "outdated"
	// When installing package for a different os/platform its not possible to verify the version
	VersionStatusUnsupportedPlatform VersionStatus = "unsupported_platform"
)

func (s VersionStatus) Pretty() api.Text {
	switch s {
	case VersionStatusValid:
		return clicky.Text("").Add(icons.Success).Append(" Up-to-date", "text-green-500")
	case VersionStatusInvalid:
		return clicky.Text("").Add(icons.Warning).Append(" Outdated", "text-yellow-500")
	case VersionStatusUnsupportedPlatform:
		return clicky.Text("").Add(icons.Skip).Append(" Unsupported Platform", "text-blue-500")
	default:
		return clicky.Text(string(s))
	}
}

type InstallResult struct {
	// Package is the package definition that was installed
	Package Package `json:"package,omitempty"`
	// Options contains the installation options that were used
	Options InstallOptions `json:"options,omitempty"`
	// Version is the version information for the installed package
	Version Version `json:"version,omitempty"`
	// Platform is the target platform for this installation
	Platform platform.Platform `json:"platform,omitempty"`
	// AppDir is the absolute path where directory-mode packages are installed (empty for binary mode)
	AppDir string `json:"app_dir,omitempty"`
	// BinDir is the absolute path to the binary directory where symlinks/wrappers/binaries are installed
	BinDir string `json:"bin_dir,omitempty"`
	// VersionOuptut is the output from running the version command
	VersionOuptut string `json:"version_ouptut,omitempty"`
	// Status indicates the installation outcome (installed, already_installed, failed, etc.)
	Status InstallStatus `json:"status,omitempty"`
	// VersionStatus indicates whether the installed version matches the requested constraint
	VersionStatus VersionStatus `json:"version_status,omitempty"`
	// VerifyStatus indicates the result of checksum verification
	VerifyStatus VerifyStatus `json:"verify_status,omitempty"`
	// DownloadURL is the final URL used for download (after following redirects)
	DownloadURL string `json:"download_url,omitempty"`
	// ChecksumURL is the URL used to download the checksum file
	ChecksumURL string `json:"checksum_url,omitempty"`
	// Duration is the total time taken for the installation
	Duration time.Duration `json:"duration,omitempty"`
	// Error contains any error encountered during installation
	Error error `json:"error,omitempty"`
	// DownloadSize is the size of the downloaded file in bytes
	DownloadSize int64 `json:"download_size,omitempty"`
	// InstalledSize is the total size of the installed binary or directory in bytes
	InstalledSize int64 `json:"installed_size,omitempty"`
	// Checksum is the SHA256 checksum of the downloaded file
	Checksum string `json:"checksum,omitempty"`
}

func relativeDir(base string) string {
	if base == "" {
		return ""
	}
	cwd, _ := os.Getwd()
	rel, err := filepath.Rel(cwd, base)
	if err != nil || strings.HasPrefix(rel, "..") {
		return base
	}
	return rel
}

func (r InstallResult) Pretty() api.Text {
	text := clicky.Text("")

	// Status line with package name and version
	packageInfo := r.Package.Name
	if r.Version.Version != "" {
		packageInfo += "@" + r.Version.Version
	}
	if r.Platform.OS != "" || r.Platform.Arch != "" {
		packageInfo += " (" + r.Platform.String() + ")"
	}

	text = text.Add(r.Status.Pretty()).Append(": " + packageInfo)

	// Show error if present
	if r.Error != nil {
		text = text.Append(r.Error.Error(), "text-red-500")
		return text
	}

	// Installation path
	if r.BinDir != "" {
		text = text.Append(" to: ", "muted").Append(relativeDir(r.BinDir))
	}
	if r.AppDir != "" && r.BinDir != r.AppDir && r.Package.Mode == "directory" {
		text = text.Append(" app-dir: ", "muted").Append(relativeDir(r.AppDir))
	}

	// Version and verify status
	if r.VersionStatus != "" {
		text = text.Add(r.VersionStatus.Pretty())
	}

	// Performance metrics
	if r.Duration > 0 {
		text = text.Append(" in ", "muted").Printf("%s", r.Duration)
	}
	if r.DownloadSize > 0 {
		text = text.Append(" downloaded: ", "muted").Append(formatBytes(r.DownloadSize))
	}

	return text
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// InstalledInfo represents information about an installed binary
type InstalledInfo struct {
	// Version is the resolved version after applying constraints
	Version string `json:"version" yaml:"version"`
	// Path is the absolute file path to the installed binary
	Path string `json:"path" yaml:"path"`
	// Platform identifies the OS and architecture of the installed binary
	Platform platform.Platform `json:"platform" yaml:"platform"`
	// Checksum is the SHA256 checksum of the installed file
	Checksum string `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	// Size is the file size in bytes
	Size int64 `json:"size,omitempty" yaml:"size,omitempty"`
	// ModTime is the last modification timestamp of the file
	ModTime time.Time `json:"mod_time" yaml:"mod_time"`
}
