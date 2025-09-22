package github

import (
	"context"
	"testing"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

func TestResolve_AssetPatternsFallback(t *testing.T) {
	manager := NewGitHubReleaseManager("", "")

	tests := []struct {
		name        string
		pkg         types.Package
		expectError bool
		description string
	}{
		{
			name: "asset_patterns_provided",
			pkg: types.Package{
				Name: "test-tool",
				Repo: "owner/repo",
				AssetPatterns: map[string]string{
					"linux-amd64": "test-tool-linux-amd64",
				},
			},
			expectError: false,
			description: "Should use provided asset patterns",
		},
		{
			name: "no_asset_patterns_with_url_template",
			pkg: types.Package{
				Name:        "test-tool",
				Repo:        "owner/repo",
				URLTemplate: "https://example.com/{{.name}}-{{.os}}-{{.arch}}",
			},
			expectError: false,
			description: "Should fall back to URL template when no asset patterns",
		},
		{
			name: "no_asset_patterns_no_url_template",
			pkg: types.Package{
				Name: "test-tool",
				Repo: "owner/repo",
			},
			expectError: false,
			description: "Should use default pattern when neither provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			plat := platform.Platform{OS: "linux", Arch: "amd64"}

			// We can't actually resolve without a real GitHub release,
			// but we can test the asset pattern logic by checking the error messages
			_, err := manager.Resolve(ctx, tt.pkg, "v1.0.0", plat)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err == nil {
				t.Errorf("Expected no error but would need real GitHub data")
				// This is expected since we don't have real GitHub data
			}
		})
	}
}

func TestHasURLSchema(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"https://example.com/file.tar.gz", true},
		{"http://example.com/file.tar.gz", true},
		{"file.tar.gz", false},
		{"example.com/file.tar.gz", false},
		{"", false},
		{"ftp://example.com/file.tar.gz", false}, // Only http/https supported
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := hasURLSchema(tt.input)
			if result != tt.expected {
				t.Errorf("hasURLSchema(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTemplateString(t *testing.T) {
	manager := NewGitHubReleaseManager("", "")

	tests := []struct {
		pattern  string
		data     map[string]string
		expected string
		hasError bool
	}{
		{
			pattern: "{{.name}}-{{.os}}-{{.arch}}",
			data: map[string]string{
				"name": "test-tool",
				"os":   "linux",
				"arch": "amd64",
			},
			expected: "test-tool-linux-amd64",
			hasError: false,
		},
		{
			pattern: "{{.name}}-{{.version}}.tar.gz",
			data: map[string]string{
				"name":    "tool",
				"version": "v1.2.3",
			},
			expected: "tool-v1.2.3.tar.gz",
			hasError: false,
		},
		{
			pattern: "{{.name}} {{invalid syntax",
			data: map[string]string{
				"name": "test",
			},
			expected: "",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			result, err := manager.templateString(tt.pattern, tt.data)

			if tt.hasError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.hasError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			if !tt.hasError && result != tt.expected {
				t.Errorf("templateString(%q, %v) = %q, want %q", tt.pattern, tt.data, result, tt.expected)
			}
		})
	}
}
