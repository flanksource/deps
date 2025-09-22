package config

import (
	"sync"

	"github.com/flanksource/deps/pkg/types"
)

var (
	globalRegistry     *types.DepsConfig
	globalRegistryOnce sync.Once
)

// GetGlobalRegistry returns the merged global registry (defaults + user config)
func GetGlobalRegistry() *types.DepsConfig {
	globalRegistryOnce.Do(func() {
		var err error
		globalRegistry, err = LoadMergedConfig("")
		if err != nil {
			// Fallback to defaults only if there's an error
			globalRegistry, _ = LoadDefaultConfig()
		}
	})
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

// ResetGlobalRegistry resets the global registry cache (useful for testing)
func ResetGlobalRegistry() {
	globalRegistryOnce = sync.Once{}
	globalRegistry = nil
}