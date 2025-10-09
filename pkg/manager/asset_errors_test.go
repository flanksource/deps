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
				// Note: Assets will be sorted by Levenshtein distance and show distance/score
				"tool-linux-arm64 [distance:",
				"tool-darwin-amd64 [distance:",
				"tool-windows-amd64.exe [distance:",
				"checksums.txt [distance:",
				"similarity:",
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
				"asset-a [distance:",
				"[distance:", // Just check that distance is shown
				"similarity:",
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
				"tool-linux-amd64.tar.gz [distance:",
				"similarity:",
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
