package platform

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
)

// Platform represents a target OS/Architecture combination
type Platform struct {
	OS   string `json:"os" yaml:"os"`
	Arch string `json:"arch" yaml:"arch"`
}

// Global overrides for platform detection
var (
	globalOSOverride   string
	globalArchOverride string
	globalMutex        sync.RWMutex
)

// String returns a string representation of the platform (e.g., "linux-amd64")
func (p Platform) String() string {
	return fmt.Sprintf("%s-%s", p.OS, p.Arch)
}

// SetGlobalOverrides sets global OS and architecture overrides from CLI flags
func SetGlobalOverrides(osOverride, archOverride string) {
	globalMutex.Lock()
	defer globalMutex.Unlock()
	globalOSOverride = osOverride
	globalArchOverride = archOverride
}

// Current returns the current platform, respecting global overrides
func Current() Platform {
	globalMutex.RLock()
	defer globalMutex.RUnlock()

	os := globalOSOverride
	arch := globalArchOverride

	if os == "" {
		os = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}

	return Platform{
		OS:   os,
		Arch: arch,
	}
}

// Parse parses a platform string (e.g., "linux-amd64") into a Platform
func Parse(platformStr string) (Platform, error) {
	parts := strings.Split(platformStr, "-")
	if len(parts) != 2 {
		return Platform{}, fmt.Errorf("invalid platform format: %s (expected os-arch)", platformStr)
	}
	return Platform{
		OS:   parts[0],
		Arch: parts[1],
	}, nil
}

// ParseList parses a list of platform strings
func ParseList(platforms []string) ([]Platform, error) {
	result := make([]Platform, len(platforms))
	for i, p := range platforms {
		platform, err := Parse(p)
		if err != nil {
			return nil, err
		}
		result[i] = platform
	}
	return result, nil
}

// CommonPlatforms returns a list of commonly supported platforms
func CommonPlatforms() []Platform {
	return []Platform{
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
		{OS: "linux", Arch: "386"},
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "windows", Arch: "amd64"},
		{OS: "windows", Arch: "386"},
		{OS: "windows", Arch: "arm64"},
	}
}

// NormalizePlatform normalizes platform values to standard forms
func (p Platform) Normalize() Platform {
	normalized := Platform{
		OS:   normalizeOS(p.OS),
		Arch: normalizeArch(p.Arch),
	}
	return normalized
}

// normalizeOS converts OS names to standard forms
func normalizeOS(os string) string {
	switch strings.ToLower(os) {
	case "macos", "osx", "mac":
		return "darwin"
	case "win", "win32", "win64":
		return "windows"
	default:
		return strings.ToLower(os)
	}
}

// normalizeArch converts architecture names to standard forms
func normalizeArch(arch string) string {
	switch strings.ToLower(arch) {
	case "x86_64", "x64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "i386", "i686", "x86", "386":
		return "386"
	case "armv7", "armv7l", "arm":
		return "arm"
	default:
		return strings.ToLower(arch)
	}
}

// IsWindows returns true if the platform is Windows
func (p Platform) IsWindows() bool {
	return p.OS == "windows"
}

// BinaryExtension returns the binary extension for the platform
func (p Platform) BinaryExtension() string {
	if p.IsWindows() {
		return ".exe"
	}
	return ""
}

// AddExtension adds the appropriate binary extension to a filename
func (p Platform) AddExtension(filename string) string {
	ext := p.BinaryExtension()
	if ext == "" || strings.HasSuffix(filename, ext) {
		return filename
	}
	return filename + ext
}
