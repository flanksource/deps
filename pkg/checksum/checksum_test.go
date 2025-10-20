package checksum

import (
	"context"
	"testing"
)

func TestEvaluateCELExpression_MapResult(t *testing.T) {
	tests := []struct {
		name           string
		expr           string
		vars           map[string]interface{}
		wantValue      string
		wantHashType   HashType
		wantURL        string
		wantErr        bool
		errContains    string
	}{
		{
			name: "valid JSON map with url and checksum",
			expr: `{'url': 'https://example.com/file.tar.gz', 'checksum': 'sha256:abc123'}`,
			vars: map[string]interface{}{},
			wantValue:    "abc123",
			wantHashType: HashTypeSHA256,
			wantURL:      "https://example.com/file.tar.gz",
			wantErr:      false,
		},
		{
			name: "Grafana-style expression with filters",
			expr: `json.packages.filter(p, p.os == os && p.arch == arch).size() > 0 ? {
				'url': json.packages.filter(p, p.os == os && p.arch == arch)[0].url,
				'checksum': 'sha256:' + json.packages.filter(p, p.os == os && p.arch == arch)[0].sha256
			} : {}`,
			vars: map[string]interface{}{
				"os":   "darwin",
				"arch": "amd64",
				"json": map[string]interface{}{
					"packages": []interface{}{
						map[string]interface{}{
							"os":     "darwin",
							"arch":   "amd64",
							"url":    "https://dl.grafana.com/grafana/release/12.2.0/grafana_12.2.0_17949786146_darwin_amd64.tar.gz",
							"sha256": "eb0e8f7419e238e8ca85aae1353f8db56bf3a47315f5a8b7b8e92c88f68295a6",
						},
					},
				},
			},
			wantValue:    "eb0e8f7419e238e8ca85aae1353f8db56bf3a47315f5a8b7b8e92c88f68295a6",
			wantHashType: HashTypeSHA256,
			wantURL:      "https://dl.grafana.com/grafana/release/12.2.0/grafana_12.2.0_17949786146_darwin_amd64.tar.gz",
			wantErr:      false,
		},
		{
			name:        "plain checksum string (backward compatibility)",
			expr:        `'sha256:def4567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef'`,
			vars:        map[string]interface{}{},
			wantValue:   "def4567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantHashType: HashTypeSHA256,
			wantURL:     "",
			wantErr:     false,
		},
		{
			name: "invalid result containing structured data",
			expr: `'url:https://example.com checksum:sha256:abc'`,
			vars: map[string]interface{}{},
			wantErr:     true,
			errContains: "does not look like a valid checksum",
		},
		{
			name:        "invalid checksum format",
			expr:        `'not-a-checksum'`,
			vars:        map[string]interface{}{},
			wantErr:     true,
			errContains: "does not look like a valid checksum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, hashType, url, err := EvaluateCELExpression(tt.vars, tt.expr)

			if tt.wantErr {
				if err == nil {
					t.Errorf("EvaluateCELExpression() expected error but got none")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("EvaluateCELExpression() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("EvaluateCELExpression() unexpected error = %v", err)
				return
			}

			if value != tt.wantValue {
				t.Errorf("EvaluateCELExpression() value = %v, want %v", value, tt.wantValue)
			}
			if hashType != tt.wantHashType {
				t.Errorf("EvaluateCELExpression() hashType = %v, want %v", hashType, tt.wantHashType)
			}
			if url != tt.wantURL {
				t.Errorf("EvaluateCELExpression() url = %v, want %v", url, tt.wantURL)
			}
		})
	}
}

func TestEvaluateCELExpression_EmptyMapResult(t *testing.T) {
	// Test case where CEL expression returns empty map (e.g., when filter doesn't match)
	expr := `json.packages.filter(p, p.os == os && p.arch == arch).size() > 0 ? {
		'url': json.packages.filter(p, p.os == os && p.arch == arch)[0].url,
		'checksum': 'sha256:' + json.packages.filter(p, p.os == os && p.arch == arch)[0].sha256
	} : {}`

	vars := map[string]interface{}{
		"os":   "linux",
		"arch": "arm64",
		"json": map[string]interface{}{
			"packages": []interface{}{
				map[string]interface{}{
					"os":     "darwin",
					"arch":   "amd64",
					"url":    "https://example.com/file.tar.gz",
					"sha256": "abc123",
				},
			},
		},
	}

	_, _, _, err := EvaluateCELExpression(vars, expr)
	if err == nil {
		t.Error("EvaluateCELExpression() expected error for empty map result, got none")
	}
	if !contains(err.Error(), "missing 'checksum' field") {
		t.Errorf("EvaluateCELExpression() error = %v, want error about missing checksum field", err)
	}
}

func TestParseChecksumFile(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		fileURL      string
		wantValue    string
		wantHashType HashType
		wantErr      bool
	}{
		{
			name: "goreleaser format",
			content: `abc123  file.tar.gz
def456  other.zip`,
			fileURL:      "https://example.com/file.tar.gz",
			wantValue:    "abc123",
			wantHashType: HashTypeSHA256,
			wantErr:      false,
		},
		{
			name: "single checksum file",
			content:      "sha256:abc123def456",
			fileURL:      "https://example.com/file.tar.gz",
			wantValue:    "abc123def456",
			wantHashType: HashTypeSHA256,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, hashType, err := ParseChecksumFile(tt.content, tt.fileURL)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseChecksumFile() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseChecksumFile() unexpected error = %v", err)
				return
			}

			if value != tt.wantValue {
				t.Errorf("ParseChecksumFile() value = %v, want %v", value, tt.wantValue)
			}
			if hashType != tt.wantHashType {
				t.Errorf("ParseChecksumFile() hashType = %v, want %v", hashType, tt.wantHashType)
			}
		})
	}
}

func TestCalculateFileChecksum_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test actual URL download and checksum calculation
	url := "https://raw.githubusercontent.com/flanksource/deps/main/README.md"

	ctx := context.Background()
	checksum, size, err := CalculateFileChecksum(ctx, url)
	if err != nil {
		t.Fatalf("CalculateFileChecksum() error = %v", err)
	}

	if checksum == "" {
		t.Error("CalculateFileChecksum() returned empty checksum")
	}
	if size == 0 {
		t.Error("CalculateFileChecksum() returned zero size")
	}
	if !contains(checksum, "sha256:") {
		t.Errorf("CalculateFileChecksum() checksum = %v, want sha256: prefix", checksum)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
