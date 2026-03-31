package config

import (
	"sync"

	"github.com/flanksource/deps/pkg/types"
)

var (
	globalRegistry     *types.DepsConfig
	globalRegistryOnce sync.Once
)

func initGlobalRegistry() {
	defaultConfig, err := LoadDefaultConfig()
	if err != nil {
		defaultConfig = &types.DepsConfig{
			Registry:     make(map[string]types.Package),
			Dependencies: make(map[string]string),
		}
	}

	userConfig, err := loadRawConfig("")
	if err != nil {
		globalRegistry = defaultConfig
	} else {
		globalRegistry = MergeWithDefaults(defaultConfig, userConfig)
	}

	applyConfigPostProcessing(globalRegistry)
}

// GetGlobalRegistry returns the pre-loaded global registry (defaults + user config).
func GetGlobalRegistry() *types.DepsConfig {
	globalRegistryOnce.Do(initGlobalRegistry)
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
	globalRegistryOnce = sync.Once{}
}

// SetGlobalRegistry sets the global registry (useful for testing)
func SetGlobalRegistry(config *types.DepsConfig) {
	globalRegistry = config
}
