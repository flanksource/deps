package manager

import (
	"fmt"
	"strings"
	"testing"

	"github.com/flanksource/deps/pkg/types"
)

func TestSuggestClosestVersion(t *testing.T) {
	availableVersions := []types.Version{
		{Version: "3.14.0", Tag: "v3.14.0", Prerelease: false},
		{Version: "3.13.0", Tag: "v3.13.0", Prerelease: false},
		{Version: "3.12.0", Tag: "v3.12.0", Prerelease: false},
		{Version: "3.15.0-rc1", Tag: "v3.15.0-rc1", Prerelease: true},
		{Version: "4.0.0", Tag: "v4.0.0", Prerelease: false},
	}

	tests := []struct {
		name             string
		requestedVersion string
		want             string
	}{
		{
			name:             "exact match",
			requestedVersion: "3.14.0",
			want:             "v3.14.0",
		},
		{
			name:             "close minor version",
			requestedVersion: "3.14.1",
			want:             "v3.14.0",
		},
		{
			name:             "close patch version",
			requestedVersion: "3.13.5",
			want:             "v3.13.0",
		},
		{
			name:             "major version diff suggests latest",
			requestedVersion: "2.0.0",
			want:             "v4.0.0", // Latest stable since major diff
		},
		{
			name:             "invalid semver",
			requestedVersion: "invalid",
			want:             "v4.0.0", // Latest stable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuggestClosestVersion(tt.requestedVersion, availableVersions)
			if got != tt.want {
				t.Errorf("SuggestClosestVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnhanceErrorWithVersions(t *testing.T) {
	availableVersions := []types.Version{
		{Version: "3.14.0", Tag: "v3.14.0", Prerelease: false},
		{Version: "3.13.0", Tag: "v3.13.0", Prerelease: false},
		{Version: "3.15.0-rc1", Tag: "v3.15.0-rc1", Prerelease: true},
	}

	originalErr := fmt.Errorf("version not found")
	enhancedErr := EnhanceErrorWithVersions("sops", "3.4.0", availableVersions, originalErr)

	errorMsg := enhancedErr.Error()

	// Check that error message contains expected components
	if !strings.Contains(errorMsg, "Version 3.4.0 not found for sops") {
		t.Errorf("Error should mention the requested version and package")
	}

	if !strings.Contains(errorMsg, "Available versions") {
		t.Errorf("Error should list available versions")
	}

	if !strings.Contains(errorMsg, "v3.14.0") {
		t.Errorf("Error should include actual available versions")
	}

	if !strings.Contains(errorMsg, "Did you mean:") {
		t.Errorf("Error should include suggestion")
	}

	if !strings.Contains(errorMsg, "(prerelease)") {
		t.Errorf("Error should mark prerelease versions")
	}
}

func TestEnhanceErrorWithVersions_NoVersions(t *testing.T) {
	var emptyVersions []types.Version
	originalErr := fmt.Errorf("version not found")

	enhancedErr := EnhanceErrorWithVersions("sops", "3.4.0", emptyVersions, originalErr)

	errorMsg := enhancedErr.Error()
	if !strings.Contains(errorMsg, "No versions found for sops") {
		t.Errorf("Error should mention no versions found")
	}
}

func TestSuggestClosestVersion_EmptyVersions(t *testing.T) {
	var emptyVersions []types.Version
	suggestion := SuggestClosestVersion("3.4.0", emptyVersions)

	if suggestion != "" {
		t.Errorf("Should return empty string for no available versions, got: %s", suggestion)
	}
}

func TestSuggestClosestVersion_OnlyPrereleases(t *testing.T) {
	prereleaseVersions := []types.Version{
		{Version: "3.15.0-rc1", Tag: "v3.15.0-rc1", Prerelease: true},
		{Version: "3.14.0-beta", Tag: "v3.14.0-beta", Prerelease: true},
	}

	suggestion := SuggestClosestVersion("3.14.0", prereleaseVersions)

	// Should fall back to first version when no stable versions available
	if suggestion != "v3.15.0-rc1" {
		t.Errorf("Should suggest first version when only prereleases available, got: %s", suggestion)
	}
}