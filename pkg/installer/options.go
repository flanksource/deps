package installer

import (
	"os"
	"time"

	"github.com/flanksource/deps/pkg/types"
)

// InstallOptions configures the installation behavior
type InstallOptions struct {
	BinDir         string
	AppDir         string
	TmpDir         string
	CacheDir       string
	Force          bool
	SkipChecksum   bool
	StrictChecksum bool // If true, checksum failures cause installation to fail
	Debug          bool
	OSOverride     string
	ArchOverride   string
	// Legacy compatibility
	VersionCheck types.VersionCheckMode
	Timeout      time.Duration
	PreferLocal  bool
}

// InstallOption is a functional option for configuring installation
type InstallOption func(*InstallOptions)

// WithBinDir sets the binary installation directory
func WithBinDir(dir string) InstallOption {
	return func(opts *InstallOptions) {
		opts.BinDir = dir
	}
}

// WithAppDir sets the application directory for directory-mode packages
func WithAppDir(dir string) InstallOption {
	return func(opts *InstallOptions) {
		opts.AppDir = dir
	}
}

// WithTmpDir sets the temporary directory for downloads and extraction
func WithTmpDir(dir string) InstallOption {
	return func(opts *InstallOptions) {
		opts.TmpDir = dir
	}
}

// WithCacheDir sets the cache directory for downloads
func WithCacheDir(dir string) InstallOption {
	return func(opts *InstallOptions) {
		opts.CacheDir = dir
	}
}

// WithForce enables or disables forced reinstallation
func WithForce(force bool) InstallOption {
	return func(opts *InstallOptions) {
		opts.Force = force
	}
}

// WithSkipChecksum enables or disables checksum verification
func WithSkipChecksum(skip bool) InstallOption {
	return func(opts *InstallOptions) {
		opts.SkipChecksum = skip
	}
}

// WithStrictChecksum enables or disables strict checksum validation
// When enabled, checksum validation failures will cause the installation to fail
// When disabled (default), checksum failures log a warning and continue without verification
func WithStrictChecksum(strict bool) InstallOption {
	return func(opts *InstallOptions) {
		opts.StrictChecksum = strict
	}
}

// WithDebug enables debug mode, keeping downloaded and extracted files
func WithDebug(debug bool) InstallOption {
	return func(opts *InstallOptions) {
		opts.Debug = debug
	}
}

// WithOS sets OS and architecture overrides
func WithOS(os, arch string) InstallOption {
	return func(opts *InstallOptions) {
		opts.OSOverride = os
		opts.ArchOverride = arch
	}
}

// WithVersionCheck sets the version checking mode (legacy compatibility)
func WithVersionCheck(mode types.VersionCheckMode) InstallOption {
	return func(opts *InstallOptions) {
		opts.VersionCheck = mode
	}
}

// WithTimeout sets the download timeout (legacy compatibility)
func WithTimeout(timeout time.Duration) InstallOption {
	return func(opts *InstallOptions) {
		opts.Timeout = timeout
	}
}

// WithPreferLocal prefers locally installed binaries over downloading (legacy compatibility)
func WithPreferLocal(prefer bool) InstallOption {
	return func(opts *InstallOptions) {
		opts.PreferLocal = prefer
	}
}

// DefaultOptions returns sensible default options
func DefaultOptions() InstallOptions {
	home, err := os.UserHomeDir()
	defaultAppDir := "/opt"
	if err == nil && os.Geteuid() != 0 {
		defaultAppDir = home + "/.local/opt"
	}

	return InstallOptions{
		BinDir:         "/usr/local/bin",
		AppDir:         defaultAppDir,
		TmpDir:         os.TempDir(),
		Force:          false,
		SkipChecksum:   false,
		StrictChecksum: true, // Default to strict checksum validation
		Debug:          false,
		OSOverride:     "",
		ArchOverride:   "",
		VersionCheck:   types.VersionCheckNone,
		Timeout:        5 * time.Minute,
		PreferLocal:    false,
	}
}
