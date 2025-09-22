package config

import (
	_ "embed"
	"fmt"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"gopkg.in/yaml.v3"
)

//go:embed defaults.yaml
var defaultDepsYAML []byte

// LoadDefaultConfig loads the embedded default configuration
func LoadDefaultConfig() (*types.DepsConfig, error) {
	var config types.DepsConfig
	if err := yaml.Unmarshal(defaultDepsYAML, &config); err != nil {
		return nil, fmt.Errorf("failed to parse embedded default deps config: %w", err)
	}

	// Apply the same package defaults as LoadDepsConfig
	if config.Registry == nil {
		config.Registry = make(map[string]types.Package)
	}
	if config.Dependencies == nil {
		config.Dependencies = make(map[string]string)
	}

	// Apply package defaults
	for name, pkg := range config.Registry {
		// Set package name to registry key if not specified
		if pkg.Name == "" {
			pkg.Name = name
		}

		// Auto-detect manager if not specified
		if pkg.Manager == "" {
			if pkg.Repo != "" {
				pkg.Manager = "github_release"
			} else if pkg.URLTemplate != "" {
				pkg.Manager = "direct"
			} else if pkg.Extra != nil {
				if _, hasImage := pkg.Extra["image"]; hasImage {
					pkg.Manager = "docker"
				} else if _, hasGroupId := pkg.Extra["group_id"]; hasGroupId {
					pkg.Manager = "maven"
				}
			}
		}

		// Update the package in the registry
		config.Registry[name] = pkg
	}

	return &config, nil
}

// mergePackage intelligently merges a user package with default package.
// User-provided fields override defaults, but default fields are preserved if not specified.
func mergePackage(defaultPkg, userPkg types.Package) types.Package {
	merged := defaultPkg // Start with all default fields

	// Override with non-empty user fields
	if userPkg.Name != "" {
		merged.Name = userPkg.Name
	}
	if userPkg.Manager != "" {
		merged.Manager = userPkg.Manager
	}
	if userPkg.Repo != "" {
		merged.Repo = userPkg.Repo
	}
	if userPkg.URLTemplate != "" {
		merged.URLTemplate = userPkg.URLTemplate
	}
	if userPkg.ChecksumFile != "" {
		merged.ChecksumFile = userPkg.ChecksumFile
	}
	if userPkg.VersionCommand != "" {
		merged.VersionCommand = userPkg.VersionCommand
	}
	if userPkg.VersionPattern != "" {
		merged.VersionPattern = userPkg.VersionPattern
	}
	if userPkg.BinaryName != "" {
		merged.BinaryName = userPkg.BinaryName
	}
	if len(userPkg.AssetPatterns) > 0 {
		merged.AssetPatterns = userPkg.AssetPatterns
	}
	if userPkg.Extra != nil && len(userPkg.Extra) > 0 {
		if merged.Extra == nil {
			merged.Extra = make(map[string]interface{})
		}
		for k, v := range userPkg.Extra {
			merged.Extra[k] = v
		}
	}

	return merged
}

// MergeWithDefaults merges the default config with a user config.
// User config takes precedence over defaults.
func MergeWithDefaults(defaultConfig, userConfig *types.DepsConfig) *types.DepsConfig {
	merged := &types.DepsConfig{
		Dependencies: make(map[string]string),
		Registry:     make(map[string]types.Package),
		Settings:     defaultConfig.Settings, // Start with default settings
	}

	// Copy default registry entries
	for name, pkg := range defaultConfig.Registry {
		merged.Registry[name] = pkg
	}

	// Copy default dependencies
	for name, version := range defaultConfig.Dependencies {
		merged.Dependencies[name] = version
	}

	// Override with user config
	if userConfig != nil {
		// User registry entries override defaults (with intelligent merging)
		for name, userPkg := range userConfig.Registry {
			if defaultPkg, exists := merged.Registry[name]; exists {
				merged.Registry[name] = mergePackage(defaultPkg, userPkg)
			} else {
				merged.Registry[name] = userPkg
			}
		}

		// User dependencies override defaults
		for name, version := range userConfig.Dependencies {
			merged.Dependencies[name] = version
		}

		// Merge settings (user settings override defaults)
		if userConfig.Settings.BinDir != "" {
			merged.Settings.BinDir = userConfig.Settings.BinDir
		}
		if userConfig.Settings.CacheDir != "" {
			merged.Settings.CacheDir = userConfig.Settings.CacheDir
		}
		if userConfig.Settings.Platform.OS != "" {
			merged.Settings.Platform.OS = userConfig.Settings.Platform.OS
		}
		if userConfig.Settings.Platform.Arch != "" {
			merged.Settings.Platform.Arch = userConfig.Settings.Platform.Arch
		}
		// Boolean settings from user take precedence
		merged.Settings.Parallel = userConfig.Settings.Parallel
		merged.Settings.SkipVerify = userConfig.Settings.SkipVerify
	}

	return merged
}

// LoadMergedConfig loads the default config and merges it with user config
func LoadMergedConfig(userConfigPath string) (*types.DepsConfig, error) {
	// Load default config
	defaultConfig, err := LoadDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load default config: %w", err)
	}

	// Try to load user config
	userConfig, err := LoadDepsConfig(userConfigPath)
	if err != nil {
		// If user config doesn't exist, just return defaults
		return defaultConfig, nil
	}

	// Merge configs
	merged := MergeWithDefaults(defaultConfig, userConfig)

	// Apply the same post-processing as LoadDepsConfig
	if merged.Settings.BinDir == "" {
		merged.Settings.BinDir = DefaultBinDir
	}
	if merged.Settings.Platform.OS == "" || merged.Settings.Platform.Arch == "" {
		currentPlatform := platform.Current()
		if merged.Settings.Platform.OS == "" {
			merged.Settings.Platform.OS = currentPlatform.OS
		}
		if merged.Settings.Platform.Arch == "" {
			merged.Settings.Platform.Arch = currentPlatform.Arch
		}
	}

	return merged, nil
}