package cmd

import (
	"testing"
)

func TestDetectRuntime(t *testing.T) {
	tests := []struct {
		name         string
		scriptPath   string
		expectedType string
		expectError  bool
	}{
		{
			name:         "Python script",
			scriptPath:   "script.py",
			expectedType: "python",
			expectError:  false,
		},
		{
			name:         "JavaScript script",
			scriptPath:   "script.js",
			expectedType: "node",
			expectError:  false,
		},
		{
			name:         "JavaScript module",
			scriptPath:   "script.mjs",
			expectedType: "node",
			expectError:  false,
		},
		{
			name:         "CommonJS script",
			scriptPath:   "script.cjs",
			expectedType: "node",
			expectError:  false,
		},
		{
			name:         "TypeScript script",
			scriptPath:   "script.ts",
			expectedType: "node",
			expectError:  false,
		},
		{
			name:         "TypeScript JSX",
			scriptPath:   "component.tsx",
			expectedType: "node",
			expectError:  false,
		},
		{
			name:         "Java source file",
			scriptPath:   "Main.java",
			expectedType: "java",
			expectError:  false,
		},
		{
			name:         "Java JAR file",
			scriptPath:   "application.jar",
			expectedType: "java",
			expectError:  false,
		},
		{
			name:         "Java class file",
			scriptPath:   "Main.class",
			expectedType: "java",
			expectError:  false,
		},
		{
			name:         "PowerShell script",
			scriptPath:   "script.ps1",
			expectedType: "powershell",
			expectError:  false,
		},
		{
			name:        "Unsupported extension",
			scriptPath:  "script.rb",
			expectError: true,
		},
		{
			name:        "No extension",
			scriptPath:  "script",
			expectError: true,
		},
		{
			name:         "Path with directory",
			scriptPath:   "/path/to/script.py",
			expectedType: "python",
			expectError:  false,
		},
		{
			name:         "Windows path",
			scriptPath:   "C:\\path\\to\\script.ps1",
			expectedType: "powershell",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeType, err := detectRuntime(tt.scriptPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if runtimeType != tt.expectedType {
					t.Errorf("expected runtime type %q, got %q", tt.expectedType, runtimeType)
				}
			}
		})
	}
}
