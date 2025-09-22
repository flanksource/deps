package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"gopkg.in/yaml.v3"
)

const (
	DepsFile      = "deps.yaml"
	LockFile      = "deps-lock.yaml"
	DefaultBinDir = "./bin"
)

// LoadDepsConfig loads and parses the deps.yaml configuration file
func LoadDepsConfig(path string) (*types.DepsConfig, error) {
	if path == "" {
		path = DepsFile
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read deps config file %s: %w", path, err)
	}

	var config types.DepsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse deps config file %s: %w", path, err)
	}

	// Set defaults
	if config.Settings.BinDir == "" {
		config.Settings.BinDir = DefaultBinDir
	}
	if config.Settings.Platform.OS == "" || config.Settings.Platform.Arch == "" {
		config.Settings.Platform = platform.Current()
	}
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

	// Expand paths
	if !filepath.IsAbs(config.Settings.BinDir) {
		if abs, err := filepath.Abs(config.Settings.BinDir); err == nil {
			config.Settings.BinDir = abs
		}
	}

	if config.Settings.CacheDir != "" && !filepath.IsAbs(config.Settings.CacheDir) {
		if config.Settings.CacheDir[0] == '~' {
			if home, err := os.UserHomeDir(); err == nil {
				config.Settings.CacheDir = filepath.Join(home, config.Settings.CacheDir[1:])
			}
		} else {
			if abs, err := filepath.Abs(config.Settings.CacheDir); err == nil {
				config.Settings.CacheDir = abs
			}
		}
	}

	return &config, nil
}

// SaveDepsConfig saves the deps configuration to a YAML file
func SaveDepsConfig(config *types.DepsConfig, path string) error {
	if path == "" {
		path = DepsFile
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal deps config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write deps config file %s: %w", path, err)
	}

	return nil
}

// LoadLockFile loads and parses the deps-lock.yaml file
func LoadLockFile(path string) (*types.LockFile, error) {
	if path == "" {
		path = LockFile
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read lock file %s: %w", path, err)
	}

	var lockFile types.LockFile
	if err := yaml.Unmarshal(data, &lockFile); err != nil {
		return nil, fmt.Errorf("failed to parse lock file %s: %w", path, err)
	}

	// Set defaults
	if lockFile.Dependencies == nil {
		lockFile.Dependencies = make(map[string]types.LockEntry)
	}

	return &lockFile, nil
}

// SaveLockFile saves the lock file to a YAML file
func SaveLockFile(lockFile *types.LockFile, path string) error {
	if path == "" {
		path = LockFile
	}

	data, err := yaml.Marshal(lockFile)
	if err != nil {
		return fmt.Errorf("failed to marshal lock file: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write lock file %s: %w", path, err)
	}

	return nil
}

// CreateDefaultConfig creates a default deps.yaml configuration
func CreateDefaultConfig() *types.DepsConfig {
	return &types.DepsConfig{
		Dependencies: make(map[string]string),
		Registry:     make(map[string]types.Package),
		Settings: types.Settings{
			BinDir:   DefaultBinDir,
			Platform: platform.Current(),
			Parallel: true,
		},
	}
}

// FindConfigFile searches for deps.yaml in the current and parent directories
func FindConfigFile() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		configPath := filepath.Join(dir, DepsFile)
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root directory
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("deps.yaml not found in current directory or any parent directory")
}

// FindLockFile searches for deps-lock.yaml in the current and parent directories
func FindLockFile() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		lockPath := filepath.Join(dir, LockFile)
		if _, err := os.Stat(lockPath); err == nil {
			return lockPath, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root directory
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("deps-lock.yaml not found in current directory or any parent directory")
}

// ValidateConfig validates the configuration for common errors
func ValidateConfig(config *types.DepsConfig) error {
	if config == nil {
		return fmt.Errorf("configuration is nil")
	}

	// Validate dependencies reference packages in registry
	for name, version := range config.Dependencies {
		if version == "" {
			return fmt.Errorf("dependency %s has empty version constraint", name)
		}

		if _, exists := config.Registry[name]; !exists {
			return fmt.Errorf("dependency %s not found in registry", name)
		}
	}

	// Validate registry entries
	for name, pkg := range config.Registry {
		if pkg.Manager == "" {
			return fmt.Errorf("package %s has no manager specified", name)
		}

		// Validate manager-specific requirements
		switch pkg.Manager {
		case "github_release":
			if pkg.Repo == "" {
				return fmt.Errorf("package %s uses github_release manager but has no repo specified", name)
			}
		case "direct":
			if pkg.URLTemplate == "" {
				return fmt.Errorf("package %s uses direct manager but has no url_template specified", name)
			}
		}
	}

	return nil
}

// MergeConfigs merges additional config into base config
func MergeConfigs(base, additional *types.DepsConfig) *types.DepsConfig {
	merged := &types.DepsConfig{
		Dependencies: make(map[string]string),
		Registry:     make(map[string]types.Package),
		Settings:     base.Settings,
	}

	// Merge dependencies
	for name, version := range base.Dependencies {
		merged.Dependencies[name] = version
	}
	for name, version := range additional.Dependencies {
		merged.Dependencies[name] = version
	}

	// Merge registry
	for name, pkg := range base.Registry {
		merged.Registry[name] = pkg
	}
	for name, pkg := range additional.Registry {
		merged.Registry[name] = pkg
	}

	return merged
}
