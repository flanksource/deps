package version

import (
	"context"
	"testing"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

// mockPackageManager implements PackageManager for testing
type mockPackageManager struct {
	name           string
	versions       []types.Version
	discoverError  error
}

func (m *mockPackageManager) Name() string {
	return m.name
}

func (m *mockPackageManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	if m.discoverError != nil {
		return nil, m.discoverError
	}

	versions := m.versions
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}
	return versions, nil
}

func TestVersionResolver_ResolveConstraint(t *testing.T) {
	testVersions := []types.Version{
		{Tag: "v2.1.0", Version: "2.1.0", Prerelease: false},
		{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
		{Tag: "v1.5.0", Version: "1.5.0", Prerelease: false},
		{Tag: "v1.4.0", Version: "1.4.0", Prerelease: false},
		{Tag: "v1.3.0-beta", Version: "1.3.0-beta", Prerelease: true},
	}

	tests := []struct {
		name           string
		constraint     string
		versions       []types.Version
		wantVersion    string
		wantError      bool
		errorContains  string
	}{
		{
			name:        "latest constraint returns newest stable version",
			constraint:  "latest",
			versions:    testVersions,
			wantVersion: "v2.1.0",
		},
		{
			name:        "stable constraint returns newest stable version",
			constraint:  "stable",
			versions:    testVersions,
			wantVersion: "v2.1.0",
		},
		{
			name:          "stable constraint with no stable versions fails",
			constraint:    "stable",
			versions:      []types.Version{{Tag: "v1.0.0-beta", Version: "1.0.0-beta", Prerelease: true}},
			wantError:     true,
			errorContains: "no stable versions found",
		},
		{
			name:        "exact version constraint returns exact match",
			constraint:  "v1.5.0",
			versions:    testVersions,
			wantVersion: "v1.5.0",
		},
		{
			name:        "exact version without v prefix returns match",
			constraint:  "1.5.0",
			versions:    testVersions,
			wantVersion: "v1.5.0",
		},
		{
			name:          "exact version not found returns error",
			constraint:    "v3.0.0",
			versions:      testVersions,
			wantError:     true,
			errorContains: "Version v3.0.0 not found",
		},
		{
			name:        "semver constraint ^1.0.0 returns latest 1.x",
			constraint:  "^1.0.0",
			versions:    testVersions,
			wantVersion: "v1.5.0",
		},
		{
			name:        "semver constraint ~1.4.0 returns latest 1.4.x",
			constraint:  "~1.4.0",
			versions:    testVersions,
			wantVersion: "v1.4.0",
		},
		{
			name:          "empty constraint returns error",
			constraint:    "",
			versions:      testVersions,
			wantError:     true,
			errorContains: "empty version constraint",
		},
		{
			name:          "no versions available returns error",
			constraint:    "latest",
			versions:      []types.Version{},
			wantError:     true,
			errorContains: "no versions found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &mockPackageManager{
				name:     "test",
				versions: tt.versions,
			}

			resolver := NewResolver(mgr)
			pkg := types.Package{Name: "test-pkg"}
			plat := platform.Platform{OS: "linux", Arch: "amd64"}

			got, err := resolver.ResolveConstraint(context.Background(), pkg, tt.constraint, plat)

			if tt.wantError {
				if err == nil {
					t.Errorf("ResolveConstraint() expected error but got none")
					return
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("ResolveConstraint() error = %v, want error containing %v", err, tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ResolveConstraint() unexpected error = %v", err)
				return
			}

			if got != tt.wantVersion {
				t.Errorf("ResolveConstraint() = %v, want %v", got, tt.wantVersion)
			}
		})
	}
}

func TestVersionResolver_GetOptimalLimit(t *testing.T) {
	mgr := &mockPackageManager{name: "test"}
	resolver := NewResolver(mgr)

	tests := []struct {
		constraint string
		wantLimit  int
	}{
		{"latest", 10},
		{"stable", 20},
		{"v1.2.3", 200},
		{"1.2.3", 200},
		{"^1.0.0", 100},
		{"~1.2.0", 50},
		{">=1.0.0", 100},
		{">=1.0.0 <2.0.0", 50},
		{"unknown", 50},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			got := resolver.getOptimalLimit(tt.constraint)
			if got != tt.wantLimit {
				t.Errorf("getOptimalLimit(%q) = %v, want %v", tt.constraint, got, tt.wantLimit)
			}
		})
	}
}

func TestVersionResolver_SelectBestVersion(t *testing.T) {
	testVersions := []types.Version{
		{Tag: "v2.1.0", Version: "2.1.0", Prerelease: false},
		{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
		{Tag: "v1.5.0", Version: "1.5.0", Prerelease: false},
		{Tag: "v1.4.0", Version: "1.4.0", Prerelease: false},
		{Tag: "v1.3.0-beta", Version: "1.3.0-beta", Prerelease: true},
	}

	mgr := &mockPackageManager{name: "test"}
	resolver := NewResolver(mgr)

	tests := []struct {
		name           string
		versions       []types.Version
		constraint     string
		wantVersion    string
		wantError      bool
		errorContains  string
	}{
		{
			name:        "latest with stable versions",
			versions:    testVersions,
			constraint:  "latest",
			wantVersion: "v2.1.0",
		},
		{
			name:        "latest with only prerelease",
			versions:    []types.Version{{Tag: "v1.0.0-beta", Version: "1.0.0-beta", Prerelease: true}},
			constraint:  "latest",
			wantVersion: "v1.0.0-beta",
		},
		{
			name:        "exact version match",
			versions:    testVersions,
			constraint:  "v1.5.0",
			wantVersion: "v1.5.0",
		},
		{
			name:        "semver constraint",
			versions:    testVersions,
			constraint:  "^1.0.0",
			wantVersion: "v1.5.0",
		},
		{
			name:          "no versions",
			versions:      []types.Version{},
			constraint:    "latest",
			wantError:     true,
			errorContains: "no versions available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.selectBestVersion(types.Package{}, tt.versions, tt.constraint)

			if tt.wantError {
				if err == nil {
					t.Errorf("selectBestVersion() expected error but got none")
					return
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("selectBestVersion() error = %v, want error containing %v", err, tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Errorf("selectBestVersion() unexpected error = %v", err)
				return
			}

			if got != tt.wantVersion {
				t.Errorf("selectBestVersion() = %v, want %v", got, tt.wantVersion)
			}
		})
	}
}

func TestVersionResolver_GetLatestVersion(t *testing.T) {
	testVersions := []types.Version{
		{Tag: "v2.1.0", Version: "2.1.0", Prerelease: false},
		{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
		{Tag: "v1.5.0-beta", Version: "1.5.0-beta", Prerelease: true},
		{Tag: "v1.4.0", Version: "1.4.0", Prerelease: false},
	}

	mgr := &mockPackageManager{name: "test"}
	resolver := NewResolver(mgr)

	tests := []struct {
		name        string
		versions    []types.Version
		stableOnly  bool
		wantVersion string
	}{
		{
			name:        "latest stable only",
			versions:    testVersions,
			stableOnly:  true,
			wantVersion: "v2.1.0",
		},
		{
			name:        "latest including prereleases",
			versions:    testVersions,
			stableOnly:  false,
			wantVersion: "v2.1.0",
		},
		{
			name:        "only prereleases with stable only",
			versions:    []types.Version{{Tag: "v1.0.0-beta", Version: "1.0.0-beta", Prerelease: true}},
			stableOnly:  true,
			wantVersion: "",
		},
		{
			name:        "only prereleases without stable only",
			versions:    []types.Version{{Tag: "v1.0.0-beta", Version: "1.0.0-beta", Prerelease: true}},
			stableOnly:  false,
			wantVersion: "v1.0.0-beta",
		},
		{
			name:        "empty versions",
			versions:    []types.Version{},
			stableOnly:  false,
			wantVersion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolver.getLatestVersion(tt.versions, tt.stableOnly)
			if got != tt.wantVersion {
				t.Errorf("getLatestVersion() = %v, want %v", got, tt.wantVersion)
			}
		})
	}
}

func TestVersionResolver_FindExactVersion(t *testing.T) {
	testVersions := []types.Version{
		{Tag: "v2.1.0", Version: "2.1.0", Prerelease: false},
		{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
		{Tag: "1.5.0", Version: "1.5.0", Prerelease: false}, // No v prefix
	}

	mgr := &mockPackageManager{name: "test"}
	resolver := NewResolver(mgr)

	tests := []struct {
		name          string
		versions      []types.Version
		target        string
		wantVersion   string
		wantError     bool
		errorContains string
	}{
		{
			name:        "exact tag match with v prefix",
			versions:    testVersions,
			target:      "v2.1.0",
			wantVersion: "v2.1.0",
		},
		{
			name:        "version match without v prefix",
			versions:    testVersions,
			target:      "2.1.0",
			wantVersion: "v2.1.0",
		},
		{
			name:        "tag without v prefix matches",
			versions:    testVersions,
			target:      "1.5.0",
			wantVersion: "1.5.0",
		},
		{
			name:          "version not found",
			versions:      testVersions,
			target:        "v3.0.0",
			wantError:     true,
			errorContains: "Version v3.0.0 not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.findExactVersion(types.Package{}, tt.versions, tt.target)

			if tt.wantError {
				if err == nil {
					t.Errorf("findExactVersion() expected error but got none")
					return
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("findExactVersion() error = %v, want error containing %v", err, tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Errorf("findExactVersion() unexpected error = %v", err)
				return
			}

			if got != tt.wantVersion {
				t.Errorf("findExactVersion() = %v, want %v", got, tt.wantVersion)
			}
		})
	}
}