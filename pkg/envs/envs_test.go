package envs

import (
	"testing"
)

func TestRenderEnvs(t *testing.T) {
	tests := []struct {
		name     string
		envs     map[string]string
		data     map[string]interface{}
		expected map[string]string
		wantErr  bool
	}{
		{
			name: "render simple env vars",
			envs: map[string]string{
				"TOOL_HOME":    "{{.dir}}",
				"TOOL_VERSION": "{{.version}}",
			},
			data: map[string]interface{}{
				"dir":     "/usr/local/bin",
				"version": "1.2.3",
			},
			expected: map[string]string{
				"TOOL_HOME":    "/usr/local/bin",
				"TOOL_VERSION": "1.2.3",
			},
			wantErr: false,
		},
		{
			name: "render all supported variables",
			envs: map[string]string{
				"TOOL_HOME":     "{{.dir}}",
				"TOOL_VERSION":  "{{.version}}",
				"TOOL_NAME":     "{{.name}}",
				"TOOL_OS":       "{{.os}}",
				"TOOL_ARCH":     "{{.arch}}",
				"TOOL_PLATFORM": "{{.platform}}",
			},
			data: map[string]interface{}{
				"dir":      "/opt/mytool",
				"version":  "2.0.0",
				"name":     "mytool",
				"os":       "linux",
				"arch":     "amd64",
				"platform": "linux-amd64",
			},
			expected: map[string]string{
				"TOOL_HOME":     "/opt/mytool",
				"TOOL_VERSION":  "2.0.0",
				"TOOL_NAME":     "mytool",
				"TOOL_OS":       "linux",
				"TOOL_ARCH":     "amd64",
				"TOOL_PLATFORM": "linux-amd64",
			},
			wantErr: false,
		},
		{
			name: "render complex template",
			envs: map[string]string{
				"PATH": "{{.dir}}/bin:$PATH",
			},
			data: map[string]interface{}{
				"dir": "/opt/java",
			},
			expected: map[string]string{
				"PATH": "/opt/java/bin:$PATH",
			},
			wantErr: false,
		},
		{
			name: "empty envs",
			envs: map[string]string{},
			data: map[string]interface{}{
				"dir": "/usr/local/bin",
			},
			expected: map[string]string{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RenderEnvs(tt.envs, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("RenderEnvs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(result) != len(tt.expected) {
					t.Errorf("RenderEnvs() result count = %d, expected %d", len(result), len(tt.expected))
					return
				}
				for key, expectedValue := range tt.expected {
					if result[key] != expectedValue {
						t.Errorf("RenderEnvs()[%s] = %s, expected %s", key, result[key], expectedValue)
					}
				}
			}
		})
	}
}

func TestPrintEnvs(t *testing.T) {
	envs := map[string]string{
		"JAVA_HOME":    "/opt/java",
		"JAVA_VERSION": "17.0.1",
	}

	// This test just ensures PrintEnvs doesn't panic
	// In a real test, we'd capture stdout and verify the output
	PrintEnvs(envs)
}
