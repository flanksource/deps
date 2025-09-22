package template

import (
	"testing"
)

func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		data     map[string]interface{}
		expected string
	}{
		{
			name:     "simple_variable_substitution",
			template: "{{.version}}",
			data: map[string]interface{}{
				"version": "v1.0.0",
			},
			expected: "v1.0.0",
		},
		{
			name:     "url_template_with_platform",
			template: "https://releases.hashicorp.com/terraform/{{.version}}/terraform_{{.version}}_{{.os}}_{{.arch}}.zip",
			data: map[string]interface{}{
				"version": "1.5.0",
				"os":      "linux",
				"arch":    "amd64",
			},
			expected: "https://releases.hashicorp.com/terraform/1.5.0/terraform_1.5.0_linux_amd64.zip",
		},
		{
			name:     "template_with_basic_functions",
			template: "{{.version}}-{{.os}}-{{.arch}}",
			data: map[string]interface{}{
				"version": "v1.2.3",
				"os":      "linux",
				"arch":    "amd64",
			},
			expected: "v1.2.3-linux-amd64",
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

func TestRenderCELExpression(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		data       map[string]interface{}
		expected   string
	}{
		{
			name:       "simple_cel_expression",
			expression: "version",
			data: map[string]interface{}{
				"version": "v1.0.0",
			},
			expected: "v1.0.0",
		},
		{
			name:       "cel_string_concatenation",
			expression: "version + '_' + os + '_' + arch",
			data: map[string]interface{}{
				"version": "1.5.0",
				"os":      "linux",
				"arch":    "amd64",
			},
			expected: "1.5.0_linux_amd64",
		},
		{
			name:       "cel_conditional_expression",
			expression: "version.startsWith('v') ? version.substring(1) : version",
			data: map[string]interface{}{
				"version": "v1.2.3",
			},
			expected: "1.2.3",
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

func TestTemplateString(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		data     map[string]string
		expected string
	}{
		{
			name:    "url_template_backwards_compatibility",
			pattern: "https://storage.googleapis.com/kubernetes-release/release/{{.version}}/bin/{{.os}}/{{.arch}}/kubectl",
			data: map[string]string{
				"version": "v1.28.0",
				"os":      "linux",
				"arch":    "amd64",
			},
			expected: "https://storage.googleapis.com/kubernetes-release/release/v1.28.0/bin/linux/amd64/kubectl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TemplateString(tt.pattern, tt.data)
			if err != nil {
				t.Fatalf("TemplateString() error = %v", err)
			}
			if result != tt.expected {
				t.Errorf("TemplateString() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTemplateURL(t *testing.T) {
	tests := []struct {
		name        string
		urlTemplate string
		version     string
		os          string
		arch        string
		expected    string
	}{
		{
			name:        "kubectl_url",
			urlTemplate: "https://storage.googleapis.com/kubernetes-release/release/{{.tag}}/bin/{{.os}}/{{.arch}}/kubectl",
			version:     "v1.28.0",
			os:          "linux",
			arch:        "amd64",
			expected:    "https://storage.googleapis.com/kubernetes-release/release/v1.28.0/bin/linux/amd64/kubectl",
		},
		{
			name:        "terraform_url",
			urlTemplate: "https://releases.hashicorp.com/terraform/{{.version}}/terraform_{{.version}}_{{.os}}_{{.arch}}.zip",
			version:     "1.5.0",
			os:          "linux",
			arch:        "amd64",
			expected:    "https://releases.hashicorp.com/terraform/1.5.0/terraform_1.5.0_linux_amd64.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TemplateURL(tt.urlTemplate, tt.version, tt.os, tt.arch)
			if err != nil {
				t.Fatalf("TemplateURL() error = %v", err)
			}
			if result != tt.expected {
				t.Errorf("TemplateURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTemplateStringWithCEL(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		data       map[string]string
		expected   string
	}{
		{
			name:       "cel_backwards_compatibility",
			expression: "(version.startsWith('v') ? version.substring(1) : version) + '_' + os + '_' + arch",
			data: map[string]string{
				"version": "v1.2.3",
				"os":      "linux",
				"arch":    "amd64",
			},
			expected: "1.2.3_linux_amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := TemplateStringWithCEL(tt.expression, tt.data)
			if err != nil {
				t.Fatalf("TemplateStringWithCEL() error = %v", err)
			}
			if result != tt.expected {
				t.Errorf("TemplateStringWithCEL() = %v, want %v", result, tt.expected)
			}
		})
	}
}