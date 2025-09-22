package template

import (
	"testing"
)

func TestIntegrationTemplateWithGomplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		data     map[string]interface{}
		expected string
	}{
		{
			name:     "kubernetes_url_template",
			template: "https://storage.googleapis.com/kubernetes-release/release/{{.version}}/bin/{{.os}}/{{.arch}}/kubectl",
			data: map[string]interface{}{
				"version": "v1.28.0",
				"os":      "linux",
				"arch":    "amd64",
			},
			expected: "https://storage.googleapis.com/kubernetes-release/release/v1.28.0/bin/linux/amd64/kubectl",
		},
		{
			name:     "terraform_url_template",
			template: "https://releases.hashicorp.com/terraform/{{.version}}/terraform_{{.version}}_{{.os}}_{{.arch}}.zip",
			data: map[string]interface{}{
				"version": "1.5.0",
				"os":      "darwin",
				"arch":    "arm64",
			},
			expected: "https://releases.hashicorp.com/terraform/1.5.0/terraform_1.5.0_darwin_arm64.zip",
		},
		{
			name:     "helm_asset_pattern",
			template: "helm-{{.version}}-{{.os}}-{{.arch}}.tar.gz",
			data: map[string]interface{}{
				"version": "v3.12.0",
				"os":      "linux",
				"arch":    "amd64",
			},
			expected: "helm-v3.12.0-linux-amd64.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RenderTemplate(tt.template, tt.data)
			if err != nil {
				t.Fatalf("RenderTemplate() error = %v", err)
			}
			if result != tt.expected {
				t.Errorf("RenderTemplate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIntegrationCELExpression(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		data       map[string]interface{}
		expected   string
	}{
		{
			name:       "version_manipulation",
			expression: "version + '_' + os + '_' + arch",
			data: map[string]interface{}{
				"version": "1.0.0",
				"os":      "linux",
				"arch":    "amd64",
			},
			expected: "1.0.0_linux_amd64",
		},
		{
			name:       "conditional_version_prefix_removal",
			expression: "version.startsWith('v') ? version.substring(1) + '_' + os : version + '_' + os",
			data: map[string]interface{}{
				"version": "v1.2.3",
				"os":      "darwin",
			},
			expected: "1.2.3_darwin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RenderCELExpression(tt.expression, tt.data)
			if err != nil {
				t.Fatalf("RenderCELExpression() error = %v", err)
			}
			if result != tt.expected {
				t.Errorf("RenderCELExpression() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBackwardsCompatibility(t *testing.T) {
	// Test that the backwards compatibility functions work as expected
	urlTemplate := "https://releases.hashicorp.com/terraform/{{.version}}/terraform_{{.version}}_{{.os}}_{{.arch}}.zip"

	result, err := TemplateURL(urlTemplate, "1.5.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("TemplateURL() error = %v", err)
	}

	expected := "https://releases.hashicorp.com/terraform/1.5.0/terraform_1.5.0_linux_amd64.zip"
	if result != expected {
		t.Errorf("TemplateURL() = %v, want %v", result, expected)
	}
}