package installer

import (
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/plugin"
)

// NewManagerRegistry returns the global package manager registry
func NewManagerRegistry() *manager.Registry {
	return manager.GetGlobalRegistry()
}

// GetPluginRegistry returns the global plugin registry
func GetPluginRegistry() *plugin.Registry {
	return plugin.GetGlobalRegistry()
}
