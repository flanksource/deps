package manager

import (
	"strings"
	"testing"
)

func TestEnhanceAssetNotFoundError(t *testing.T) {
	tests := []struct {
		name           string
		packageName    string
		assetPattern   string
		platform       string
		availableAssets []string
		expectedContains []string
		expectedCount    int // Expected number of assets shown
	}{
		{
			name:         "Basic asset not found with suggestions",
			packageName:  "example/tool",
			assetPattern: "tool-linux-amd64",
			platform:     "linux-amd64",
			availableAssets: []string{
				"tool-darwin-amd64",
				"tool-linux-arm64",
				"tool-windows-amd64.exe",
				"checksums.txt",
			},
			expectedContains: []string{
				"Asset not found: tool-linux-amd64 for linux-amd64 in package example/tool",
				"Available assets (4 total):",
				"tool-darwin-amd64",
				"tool-linux-arm64",
				"tool-windows-amd64.exe",
				"checksums.txt",
				"Searched for pattern: tool-linux-amd64",
				"Did you mean:", // Just check that it suggests something
			},
			expectedCount: 4,
		},
		{
			name:         "Many assets with truncation",
			packageName:  "big/project",
			assetPattern: "missing-asset",
			platform:     "linux-amd64",
			availableAssets: func() []string {
				assets := make([]string, 25)
				for i := 0; i < 25; i++ {
					assets[i] = "asset-" + string(rune('a'+i))
				}
				return assets
			}(),
			expectedContains: []string{
				"Asset not found: missing-asset for linux-amd64 in package big/project",
				"Available assets (25 total):",
				"asset-a",
				"asset-t", // 20th asset (since we limit to 20)
				"... and 5 more assets",
				"Searched for pattern: missing-asset",
			},
			expectedCount: 20,
		},
		{
			name:            "No assets available",
			packageName:     "empty/repo",
			assetPattern:    "nonexistent",
			platform:        "linux-amd64",
			availableAssets: []string{},
			expectedContains: []string{
				"No assets found for empty/repo",
			},
			expectedCount: 0,
		},
		{
			name:         "Exact match suggestion",
			packageName:  "test/tool",
			assetPattern: "tool-linux-amd64",
			platform:     "linux-amd64",
			availableAssets: []string{
				"tool-linux-amd64.tar.gz", // Very similar, should be suggested
				"tool-darwin-amd64.tar.gz",
			},
			expectedContains: []string{
				"Did you mean: tool-linux-amd64.tar.gz?",
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalErr := &ErrAssetNotFound{
				Package:         tt.packageName,
				AssetPattern:    tt.assetPattern,
				Platform:        tt.platform,
				AvailableAssets: tt.availableAssets,
			}

			enhancedErr := EnhanceAssetNotFoundError(tt.packageName, tt.assetPattern, tt.platform, tt.availableAssets, originalErr)
			errMsg := enhancedErr.Error()

			// Check that all expected strings are present
			for _, expected := range tt.expectedContains {
				if !strings.Contains(errMsg, expected) {
					t.Errorf("Enhanced error message missing expected content:\nExpected: %s\nFull message: %s", expected, errMsg)
				}
			}

			// Count actual assets shown (up to limit)
			assetCount := 0
			for _, asset := range tt.availableAssets {
				if strings.Contains(errMsg, asset) {
					assetCount++
				}
			}

			if assetCount != tt.expectedCount {
				t.Errorf("Expected %d assets to be shown, but found %d", tt.expectedCount, assetCount)
			}
		})
	}
}

func TestSuggestClosestAsset(t *testing.T) {
	tests := []struct {
		name            string
		target          string
		availableAssets []string
		expectedSuggestion string
	}{
		{
			name:   "Exact match with extension",
			target: "tool-linux-amd64",
			availableAssets: []string{
				"tool-linux-amd64.tar.gz",
				"tool-darwin-amd64.tar.gz",
			},
			expectedSuggestion: "tool-linux-amd64.tar.gz",
		},
		{
			name:   "Partial match",
			target: "myapp-linux-x64",
			availableAssets: []string{
				"myapp-linux-amd64",
				"myapp-darwin-amd64",
			},
			expectedSuggestion: "myapp-linux-amd64",
		},
		{
			name:   "No good match",
			target: "completely-different",
			availableAssets: []string{
				"other-tool",
				"another-binary",
			},
			expectedSuggestion: "",
		},
		{
			name:   "Case insensitive matching",
			target: "Tool-Linux-AMD64",
			availableAssets: []string{
				"tool-linux-amd64.zip",
			},
			expectedSuggestion: "tool-linux-amd64.zip",
		},
		{
			name:            "Empty assets",
			target:          "anything",
			availableAssets: []string{},
			expectedSuggestion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := SuggestClosestAsset(tt.target, tt.availableAssets)
			if suggestion != tt.expectedSuggestion {
				t.Errorf("Expected suggestion %q, got %q", tt.expectedSuggestion, suggestion)
			}
		})
	}
}

func TestCalculateAssetSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		candidate string
		minScore int // Minimum expected score
		maxScore int // Maximum expected score
	}{
		{
			name:      "Exact match",
			target:    "tool-linux-amd64",
			candidate: "tool-linux-amd64",
			minScore:  100,
			maxScore:  100,
		},
		{
			name:      "Extension difference",
			target:    "tool-linux-amd64",
			candidate: "tool-linux-amd64.tar.gz",
			minScore:  70,
			maxScore:  100,
		},
		{
			name:      "Substring match",
			target:    "linux-amd64",
			candidate: "tool-linux-amd64",
			minScore:  80,
			maxScore:  100,
		},
		{
			name:      "Common parts",
			target:    "myapp-linux-x64",
			candidate: "myapp-linux-amd64",
			minScore:  30,
			maxScore:  90,
		},
		{
			name:      "No similarity",
			target:    "completely-different",
			candidate: "other-file",
			minScore:  0,
			maxScore:  30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateAssetSimilarity(tt.target, tt.candidate)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("Score %d not in expected range [%d, %d] for target=%q candidate=%q",
					score, tt.minScore, tt.maxScore, tt.target, tt.candidate)
			}
		})
	}
}

func TestSplitAssetName(t *testing.T) {
	tests := []struct {
		name     string
		assetName string
		expected []string
	}{
		{
			name:      "Hyphen separated",
			assetName: "tool-linux-amd64.tar.gz",
			expected:  []string{"tool", "linux", "amd64", "tar"},
		},
		{
			name:      "Underscore separated",
			assetName: "my_app_windows_x64.exe",
			expected:  []string{"my", "app", "windows", "x64"},
		},
		{
			name:      "Mixed separators",
			assetName: "app-name_v1.2.3.tar.gz",
			expected:  []string{"app", "name", "v1", "2", "3", "tar"},
		},
		{
			name:      "No separators",
			assetName: "simpletool.exe",
			expected:  []string{"simpletool"},
		},
		{
			name:      "Empty string",
			assetName: "",
			expected:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitAssetName(tt.assetName)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d parts, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			for i, expected := range tt.expected {
				if i >= len(result) || result[i] != expected {
					t.Errorf("Expected part %d to be %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}