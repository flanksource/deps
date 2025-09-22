package plugin

import (
	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/deps/pkg/types"
)

// InstallOptions contains options for plugin installation
type InstallOptions struct {
	BinDir       string
	Force        bool
	SkipChecksum bool
	Debug        bool
	OSOverride   string
	ArchOverride string
}

// InstallPlugin defines the interface for custom installation plugins
type InstallPlugin interface {
	// Name returns the package name this plugin handles
	Name() string

	// CanHandle checks if this plugin should handle the installation for the given package
	CanHandle(name string, pkg types.Package) bool

	// Install performs the custom installation
	Install(ctx flanksourceContext.Context, name, version string, pkg types.Package, opts InstallOptions, task *task.Task) error
}

// Registry manages installation plugins
type Registry struct {
	plugins map[string]InstallPlugin
}

// NewRegistry creates a new plugin registry
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]InstallPlugin),
	}
}

// Register adds a plugin to the registry
func (r *Registry) Register(plugin InstallPlugin) {
	r.plugins[plugin.Name()] = plugin
}

// Get retrieves a plugin by name
func (r *Registry) Get(name string) (InstallPlugin, bool) {
	plugin, exists := r.plugins[name]
	return plugin, exists
}

// FindHandler finds a plugin that can handle the given package
func (r *Registry) FindHandler(name string, pkg types.Package) InstallPlugin {
	// First try exact name match
	if plugin, exists := r.plugins[name]; exists && plugin.CanHandle(name, pkg) {
		return plugin
	}

	// Then try all plugins to see if any can handle it
	for _, plugin := range r.plugins {
		if plugin.CanHandle(name, pkg) {
			return plugin
		}
	}

	return nil
}

// List returns all registered plugin names
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}

// HasHandler checks if there's a plugin that can handle the package
func (r *Registry) HasHandler(name string, pkg types.Package) bool {
	return r.FindHandler(name, pkg) != nil
}

// Global plugin registry
var globalRegistry = NewRegistry()

// Register adds a plugin to the global registry
func Register(plugin InstallPlugin) {
	globalRegistry.Register(plugin)
}

// GetGlobalRegistry returns the global plugin registry
func GetGlobalRegistry() *Registry {
	return globalRegistry
}
