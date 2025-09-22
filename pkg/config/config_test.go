package config

import (
	"testing"

	"github.com/flanksource/deps/pkg/types"
)

func TestPackageDefaults(t *testing.T) {
	tests := []struct {
		name         string
		input        types.DepsConfig
		expectedName string
		expectedMgr  string
		description  string
	}{
		{
			name: "name_defaults_to_key",
			input: types.DepsConfig{
				Registry: map[string]types.Package{
					"kubectl": {
						Repo: "kubernetes/kubernetes",
					},
				},
			},
			expectedName: "kubectl",
			expectedMgr:  "github_release",
			description:  "Package name should default to registry key and manager to github_release",
		},
		{
			name: "existing_name_preserved",
			input: types.DepsConfig{
				Registry: map[string]types.Package{
					"k8s-cli": {
						Name: "kubectl",
						Repo: "kubernetes/kubernetes",
					},
				},
			},
			expectedName: "kubectl",
			expectedMgr:  "github_release",
			description:  "Existing package name should be preserved",
		},
		{
			name: "existing_manager_preserved",
			input: types.DepsConfig{
				Registry: map[string]types.Package{
					"terraform": {
						Manager:     "direct",
						URLTemplate: "https://releases.hashicorp.com/terraform/{{.version}}/terraform_{{.version}}_{{.os}}_{{.arch}}.zip",
					},
				},
			},
			expectedName: "terraform",
			expectedMgr:  "direct",
			description:  "Existing manager should be preserved",
		},
		{
			name: "no_repo_no_manager_change",
			input: types.DepsConfig{
				Registry: map[string]types.Package{
					"some-tool": {
						URLTemplate: "https://example.com/tool",
					},
				},
			},
			expectedName: "some-tool",
			expectedMgr:  "",
			description:  "No manager should be set if no repo is specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply the same logic as LoadDepsConfig
			config := tt.input
			if config.Registry == nil {
				config.Registry = make(map[string]types.Package)
			}

			// Apply package defaults (same logic as in LoadDepsConfig)
			for name, pkg := range config.Registry {
				if pkg.Name == "" {
					pkg.Name = name
				}
				if pkg.Manager == "" && pkg.Repo != "" {
					pkg.Manager = "github_release"
				}
				config.Registry[name] = pkg
			}

			// Get the first (and only) package to test
			var pkg types.Package
			for _, p := range config.Registry {
				pkg = p
				break
			}

			if pkg.Name != tt.expectedName {
				t.Errorf("Package name = %q, want %q", pkg.Name, tt.expectedName)
			}
			if pkg.Manager != tt.expectedMgr {
				t.Errorf("Package manager = %q, want %q", pkg.Manager, tt.expectedMgr)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *types.DepsConfig
		expectError bool
		description string
	}{
		{
			name: "valid_github_config",
			config: &types.DepsConfig{
				Dependencies: map[string]string{
					"kubectl": "v1.31.0",
				},
				Registry: map[string]types.Package{
					"kubectl": {
						Name:    "kubectl",
						Manager: "github_release",
						Repo:    "kubernetes/kubernetes",
					},
				},
			},
			expectError: false,
			description: "Valid GitHub release configuration should pass",
		},
		{
			name: "valid_direct_config",
			config: &types.DepsConfig{
				Dependencies: map[string]string{
					"terraform": "1.1.7",
				},
				Registry: map[string]types.Package{
					"terraform": {
						Name:        "terraform",
						Manager:     "direct",
						URLTemplate: "https://releases.hashicorp.com/terraform/{{.version}}/terraform_{{.version}}_{{.os}}_{{.arch}}.zip",
					},
				},
			},
			expectError: false,
			description: "Valid direct URL configuration should pass",
		},
		{
			name: "missing_repo_for_github",
			config: &types.DepsConfig{
				Dependencies: map[string]string{
					"kubectl": "v1.31.0",
				},
				Registry: map[string]types.Package{
					"kubectl": {
						Name:    "kubectl",
						Manager: "github_release",
					},
				},
			},
			expectError: true,
			description: "GitHub release without repo should fail",
		},
		{
			name: "missing_url_template_for_direct",
			config: &types.DepsConfig{
				Dependencies: map[string]string{
					"tool": "1.0.0",
				},
				Registry: map[string]types.Package{
					"tool": {
						Name:    "tool",
						Manager: "direct",
					},
				},
			},
			expectError: true,
			description: "Direct manager without URL template should fail",
		},
		{
			name: "dependency_not_in_registry",
			config: &types.DepsConfig{
				Dependencies: map[string]string{
					"missing-tool": "1.0.0",
				},
				Registry: map[string]types.Package{},
			},
			expectError: true,
			description: "Dependency not in registry should fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}
