package config

import (
	"github.com/flanksource/deps/pkg/types"
)

var globalRegistry *types.DepsConfig

func init() {
	// Load defaults + user config during package initialization
	defaultConfig, err := LoadDefaultConfig()
	if err != nil {
		// If we can't load defaults, create minimal config
		defaultConfig = &types.DepsConfig{
			Registry:     make(map[string]types.Package),
			Dependencies: make(map[string]string),
		}
	}

	// Try to load user config
	userConfig, err := loadRawConfig("")
	if err != nil {
		// If user config doesn't exist, just use defaults
		globalRegistry = defaultConfig
	} else {
		// Merge configs
		globalRegistry = MergeWithDefaults(defaultConfig, userConfig)
	}

	// Apply post-processing (same logic as LoadMergedConfig)
	applyConfigPostProcessing(globalRegistry)
}

// GetGlobalRegistry returns the pre-loaded global registry (defaults + user config)
func GetGlobalRegistry() *types.DepsConfig {
	return globalRegistry
}

// GetPackage returns a package definition by name from the global registry
func GetPackage(name string) (types.Package, bool) {
	registry := GetGlobalRegistry()
	pkg, exists := registry.Registry[name]
	return pkg, exists
}

// ListAllPackages returns all available package names from the global registry
func ListAllPackages() []string {
	registry := GetGlobalRegistry()
	var names []string
	for name := range registry.Registry {
		names = append(names, name)
	}
	return names
}

// PackageExists checks if a package exists in the global registry
func PackageExists(name string) bool {
	_, exists := GetPackage(name)
	return exists
}

// ResetGlobalRegistry resets the global registry (useful for testing)
// Note: This will not reload the registry until the program is restarted
// since init functions only run once per program execution
func ResetGlobalRegistry() {
	globalRegistry = nil
}