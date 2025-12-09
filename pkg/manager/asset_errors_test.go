package manager

import (
	"strings"
	"testing"
)

func TestEnhanceAssetNotFoundError(t *testing.T) {
	tests := []struct {
		name             string
		packageName      string
		assetPattern     string
		platform         string
		availableAssets  []string
		expectedContains []string
		expectedCount    int // Expected number of assets shown
	}{
		{
			name:         "Basic asset not found with suggestions (filtered by OS)",
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
				"Available assets (2 total):", // Only linux + universal (checksums.txt)
				"tool-linux-arm64",
				"checksums.txt",
				"Searched for pattern: tool-linux-amd64",
				"Did you mean:", // Just check that it suggests something
			},
			expectedCount: 2, // Only linux-arm64 and checksums.txt (no OS marker)
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
				"tool-linux-arm64.tar.gz",
				"tool-darwin-amd64.tar.gz", // Will be filtered out
			},
			expectedContains: []string{
				"Available assets (2 total):", // darwin filtered out
				"tool-linux-amd64.tar.gz",
				"Did you mean: tool-linux-amd64.tar.gz?",
			},
			expectedCount: 2, // Only linux assets shown
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
		name               string
		target             string
		availableAssets    []string
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
			name:               "Empty assets",
			target:             "anything",
			availableAssets:    []string{},
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

func TestFilterAssetsByTargetOS(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		assets   []string
		expected []string
	}{
		{
			name:     "should filter out other OSes when showing darwin assets",
			platform: "darwin-arm64",
			assets: []string{
				"tool-darwin-arm64.tar.gz",
				"tool-darwin-amd64.tar.gz",
				"tool-linux-arm64.tar.gz",
				"tool-linux-amd64.tar.gz",
				"tool-windows-amd64.zip",
			},
			expected: []string{
				"tool-darwin-arm64.tar.gz",
				"tool-darwin-amd64.tar.gz",
			},
		},
		{
			name:     "should filter out other OSes when showing linux assets",
			platform: "linux-amd64",
			assets: []string{
				"tool-linux-amd64",
				"tool-darwin-amd64",
				"tool-windows-amd64.exe",
				"tool-macos-amd64",
			},
			expected: []string{
				"tool-linux-amd64",
			},
		},
		{
			name:     "should keep universal assets (no OS in name)",
			platform: "linux-amd64",
			assets: []string{
				"tool-linux-amd64",
				"universal-binary",
				"checksums.txt",
				"tool-windows-amd64.exe",
			},
			expected: []string{
				"tool-linux-amd64",
				"universal-binary",
				"checksums.txt",
			},
		},
		{
			name:     "should return all assets if filtering removes everything",
			platform: "freebsd-amd64",
			assets: []string{
				"tool-linux-amd64",
				"tool-darwin-amd64",
			},
			expected: []string{
				"tool-linux-amd64",
				"tool-darwin-amd64",
			},
		},
		{
			name:     "should handle empty platform",
			platform: "",
			assets: []string{
				"tool-linux-amd64",
				"tool-darwin-amd64",
			},
			expected: []string{
				"tool-linux-amd64",
				"tool-darwin-amd64",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterAssetsByTargetOS(tt.assets, tt.platform)
			if len(filtered) != len(tt.expected) {
				t.Errorf("Expected %d assets, got %d: %v", len(tt.expected), len(filtered), filtered)
				return
			}
			for i, exp := range tt.expected {
				if filtered[i] != exp {
					t.Errorf("Expected asset[%d] = %q, got %q", i, exp, filtered[i])
				}
			}
		})
	}
}

func TestCalculateAssetSimilarity(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		candidate string
		minScore  int // Minimum expected score based on Levenshtein distance
		maxScore  int // Maximum expected score
	}{
		{
			name:      "Exact match",
			target:    "tool-linux-amd64",
			candidate: "tool-linux-amd64",
			minScore:  100,
			maxScore:  100,
		},
		{
			name:      "Extension difference (7 chars different)",
			target:    "tool-linux-amd64",
			candidate: "tool-linux-amd64.tar.gz",
			minScore:  70, // ~7 chars difference out of ~24 = ~70% similarity
			maxScore:  75,
		},
		{
			name:      "Prefix match (5 chars different)",
			target:    "linux-amd64",
			candidate: "tool-linux-amd64",
			minScore:  68, // 5 chars difference out of 16 = ~68% similarity
			maxScore:  72,
		},
		{
			name:      "Similar with character substitutions (3 chars different)",
			target:    "myapp-linux-x64",
			candidate: "myapp-linux-amd64",
			minScore:  82, // 3 chars difference out of 17 = ~82% similarity
			maxScore:  87,
		},
		{
			name:      "No similarity",
			target:    "completely-different",
			candidate: "other-file",
			minScore:  0,
			maxScore:  30, // Levenshtein may give some similarity due to common letters
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
