package config

import (
	"embed"
	"fmt"
	"strings"

	"github.com/flanksource/deps/pkg/types"
	"gopkg.in/yaml.v3"
)

//go:embed defaults.yaml
var defaultDepsYAML []byte

//go:embed services.yaml services-kubernetes.yaml
var defaultServicesFS embed.FS

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

	if err := attachServiceSpecs(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// attachServiceSpecs merges the embedded services.yaml into the registry:
// existing entries gain a Service block, unknown keys become service-only
// entries (no installable artifact, e.g. mysql, mssql, valkey).
func attachServiceSpecs(config *types.DepsConfig) error {
	services := map[string]*types.ServiceSpec{}
	for _, filename := range []string{"services.yaml", "services-kubernetes.yaml"} {
		data, err := defaultServicesFS.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("failed to read embedded services config %s: %w", filename, err)
		}
		var document struct {
			Services map[string]*types.ServiceSpec `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("failed to parse embedded services config %s: %w", filename, err)
		}
		for name, spec := range document.Services {
			if _, exists := services[name]; exists {
				return fmt.Errorf("duplicate embedded service %q", name)
			}
			services[name] = spec
		}
	}

	for name, spec := range services {
		if err := validateServiceVolume(name, spec); err != nil {
			return err
		}
		addHelmResourceParameters(spec)
		if pkg, exists := config.Registry[name]; exists {
			pkg.Service = spec
			config.Registry[name] = pkg
		} else {
			config.Registry[name] = types.Package{Name: name, Service: spec}
		}
	}
	return nil
}

func validateServiceVolume(name string, spec *types.ServiceSpec) error {
	if spec.Helm == nil || spec.Helm.Volume == nil {
		return nil
	}
	volume := spec.Helm.Volume
	if !strings.HasPrefix(volume.MountPath, "/") {
		return fmt.Errorf("service %s helm volume mount_path must be absolute", name)
	}
	if len(volume.Modes) == 0 {
		return fmt.Errorf("service %s helm volume requires at least one mode", name)
	}
	for mode, mapping := range volume.Modes {
		switch mode {
		case "persistent", "host", "ephemeral":
		default:
			return fmt.Errorf("service %s helm volume has invalid mode %q", name, mode)
		}
		for key := range mapping.Set {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("service %s helm volume mode %s has an empty value path", name, mode)
			}
		}
	}
	return nil
}

func addHelmResourceParameters(spec *types.ServiceSpec) {
	if spec.Helm == nil || spec.Helm.ResourcePrefix == "" {
		return
	}
	if spec.Parameters == nil {
		spec.Parameters = map[string]types.ServiceParameter{}
	}
	if spec.Helm.Set == nil {
		spec.Helm.Set = map[string]string{}
	}
	definitions := map[string]struct {
		description string
		path        string
	}{
		"cpu-limit":      {"Kubernetes CPU limit", "limits.cpu"},
		"cpu-request":    {"Kubernetes CPU request", "requests.cpu"},
		"memory-limit":   {"Kubernetes memory limit", "limits.memory"},
		"memory-request": {"Kubernetes memory request", "requests.memory"},
	}
	for name, definition := range definitions {
		spec.Parameters[name] = types.ServiceParameter{
			Type:        types.ServiceParameterQuantity,
			Description: definition.description,
			Runtimes:    []string{"helm"},
		}
		spec.Helm.Set[spec.Helm.ResourcePrefix+"."+definition.path] = fmt.Sprintf(`{{index .parameters %q}}`, name)
	}
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
	if userPkg.VersionRegex != "" {
		merged.VersionRegex = userPkg.VersionRegex
	}
	if userPkg.BinaryName != "" {
		merged.BinaryName = userPkg.BinaryName
	}
	if len(userPkg.AssetPatterns) > 0 {
		merged.AssetPatterns = userPkg.AssetPatterns
	}
	if len(userPkg.Extra) > 0 {
		if merged.Extra == nil {
			merged.Extra = make(map[string]interface{})
		}
		for k, v := range userPkg.Extra {
			merged.Extra[k] = v
		}
	}
	if userPkg.Service != nil {
		merged.Service = userPkg.Service
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
	userConfig, err := loadRawConfig(userConfigPath)
	if err != nil {
		// If user config doesn't exist, just return defaults
		merged := defaultConfig
		applyConfigPostProcessing(merged)
		return merged, nil
	}

	// Merge configs
	merged := MergeWithDefaults(defaultConfig, userConfig)

	// Apply post-processing
	applyConfigPostProcessing(merged)

	return merged, nil
}
