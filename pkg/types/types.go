package types

import (
	"fmt"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/utils"
	"github.com/flanksource/gomplate/v3"
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
	VersionsURL    string                 `json:"versions_url,omitempty" yaml:"versions_url,omitempty"`       // URL to fetch version list (for url manager)
	VersionsExpr   string                 `json:"versions_expr,omitempty" yaml:"versions_expr,omitempty"`     // CEL expression to extract structured version data from JSON (returns array of {version, url?, checksum?, asset?})
	AssetPatterns  map[string]string      `json:"asset_patterns,omitempty" yaml:"asset_patterns,omitempty"`   // Platform -> asset pattern
	ChecksumFile   string                 `json:"checksum_file,omitempty" yaml:"checksum_file,omitempty"`     // Checksum file pattern (comma-separated for multiple files)
	ChecksumExpr   string                 `json:"checksum_expr,omitempty" yaml:"checksum_expr,omitempty"`     // CEL expression to extract checksum from file contents
	AssetsExpr     string                 `json:"assets_expr,omitempty" yaml:"assets_expr,omitempty"`         // CEL expression for JSON asset discovery (returns {url, checksum})
	VersionCommand string                 `json:"version_command,omitempty" yaml:"version_command,omitempty"` // Command to get version
	VersionRegex   string                 `json:"version_regex,omitempty" yaml:"version_regex,omitempty"`     // Regex to extract version
	VersionExpr    string                 `json:"version_expr,omitempty" yaml:"version_expr,omitempty"`       // CEL expression to filter and map versions
	BinaryName     string                 `json:"binary_name,omitempty" yaml:"binary_name,omitempty"`         // Custom binary name
	BinaryPath     string                 `json:"binary_path,omitempty" yaml:"binary_path,omitempty"`         // Path within archive (supports CEL expressions)
	PreInstalled   []string               `json:"pre_installed,omitempty" yaml:"pre_installed,omitempty"`     // Pre-installed binary names
	Extract        *bool                  `json:"extract,omitempty" yaml:"extract,omitempty"`                 // Override auto-detection of archive extraction (nil=auto, true=force, false=skip)
	PostProcess    []string               `json:"post_process,omitempty" yaml:"post_process,omitempty"`       // CEL pipeline operations after download (e.g., ["unarchive(glob('*.txz'))", "chdir(glob('*:dir'))"]). Supports platform prefixes: ["!windows*: rm(glob('*.bat'))"]
	Mode           string                 `json:"mode,omitempty" yaml:"mode,omitempty"`                       // Installation mode: "binary" (default) or "directory"
	Symlinks       []string               `json:"symlinks,omitempty" yaml:"symlinks,omitempty"`               // Glob patterns of paths in app-dir to symlink to bin-dir (directory mode only). Supports platform prefixes: ["windows*: bin/tool.bat", "!windows*: bin/tool"]
	WrapperScript  string                 `json:"wrapper_script,omitempty" yaml:"wrapper_script,omitempty"`   // Wrapper script template to create in bin-dir. Supports templating with {{.appDir}}, {{.binDir}}, {{.name}}, {{.version}}, {{.os}}, {{.arch}}
	Extra          map[string]interface{} `json:"extra,omitempty" yaml:"extra,omitempty"`                     // Manager-specific config
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
	VersionRegex   string                   `json:"version_regex,omitempty" yaml:"version_regex,omitempty"`
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
	AppDir     string            `json:"app_dir,omitempty" yaml:"app_dir,omitempty"`
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
	// Version are running through version_expr / normalize
	Normalized string `json:"normalized,omitempty" yaml:"normalized,omitempty"` // Normalized semver version
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
	Package  Package           `json:"package,omitempty"`
	Options  InstallOptions    `json:"options,omitempty"`
	Version  Version           `json:"version,omitempty"`
	Platform platform.Platform `json:"platform,omitempty"`
	// Absolute path where directory mode package was installed, empty for binary mode
	AppDir string `json:"app_dir,omitempty"`
	// Absolute path to the binary directory symlinks/wrappers/binaries are installed into
	BinDir        string        `json:"bin_dir,omitempty"`
	VersionOuptut string        `json:"version_ouptut,omitempty"`
	Status        InstallStatus `json:"status,omitempty"`
	VersionStatus VersionStatus `json:"version_status,omitempty"`
	VerifyStatus  VerifyStatus  `json:"verify_status,omitempty"`
	// Final URL used for download (after redirects)
	DownloadURL string `json:"download_url,omitempty"`
	ChecksumURL string `json:"checksum_url,omitempty"`
	// Time taken for the installation
	Duration time.Duration `json:"duration,omitempty"`
	// Any error encountered during installation
	Error        error `json:"error,omitempty"`
	DownloadSize int64 `json:"download_size,omitempty"`
	// Size of the installed binary or directory
	InstalledSize int64 `json:"installed_size,omitempty"`
	// Checksum of the download
	Checksum string `json:"checksum,omitempty"`
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
		text = text.Append(" bin-dir: ", "muted").Append(r.BinDir)
	}
	if r.AppDir != "" {
		text = text.Append(" app-dir: ", "muted").Append(r.AppDir)
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
	// Final resolved version after applying constraints
	Version  string            `json:"version" yaml:"version"`
	Path     string            `json:"path" yaml:"path"`
	Platform platform.Platform `json:"platform" yaml:"platform"`
	Checksum string            `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	Size     int64             `json:"size,omitempty" yaml:"size,omitempty"`
	ModTime  time.Time         `json:"mod_time" yaml:"mod_time"`
}
