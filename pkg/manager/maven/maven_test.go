package maven

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

func TestMavenManager_Name(t *testing.T) {
	manager := NewMavenManager()
	if manager.Name() != "maven" {
		t.Errorf("Expected name 'maven', got '%s'", manager.Name())
	}
}

func TestMavenManager_parseCoordinates(t *testing.T) {
	manager := NewMavenManager()

	tests := []struct {
		name    string
		pkg     types.Package
		want    *MavenCoordinates
		wantErr bool
	}{
		{
			name: "valid coordinates",
			pkg: types.Package{
				Extra: map[string]interface{}{
					"group_id":    "org.apache.commons",
					"artifact_id": "commons-lang3",
					"packaging":   "jar",
					"classifier":  "sources",
				},
			},
			want: &MavenCoordinates{
				GroupID:    "org.apache.commons",
				ArtifactID: "commons-lang3",
				Packaging:  "jar",
				Classifier: "sources",
			},
			wantErr: false,
		},
		{
			name: "minimal coordinates",
			pkg: types.Package{
				Extra: map[string]interface{}{
					"group_id":    "org.springframework",
					"artifact_id": "spring-core",
				},
			},
			want: &MavenCoordinates{
				GroupID:    "org.springframework",
				ArtifactID: "spring-core",
				Packaging:  "jar", // default
			},
			wantErr: false,
		},
		{
			name: "missing group_id",
			pkg: types.Package{
				Extra: map[string]interface{}{
					"artifact_id": "commons-lang3",
				},
			},
			wantErr: true,
		},
		{
			name: "missing artifact_id",
			pkg: types.Package{
				Extra: map[string]interface{}{
					"group_id": "org.apache.commons",
				},
			},
			wantErr: true,
		},
		{
			name:    "no extra config",
			pkg:     types.Package{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := manager.parseCoordinates(tt.pkg)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseCoordinates() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.GroupID != tt.want.GroupID ||
					got.ArtifactID != tt.want.ArtifactID ||
					got.Packaging != tt.want.Packaging ||
					got.Classifier != tt.want.Classifier {
					t.Errorf("parseCoordinates() = %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

func TestMavenManager_templateCoordinates(t *testing.T) {
	manager := NewMavenManager()
	plat := platform.Platform{OS: "linux", Arch: "amd64"}

	tests := []struct {
		name    string
		coords  *MavenCoordinates
		version string
		want    *MavenCoordinates
	}{
		{
			name: "template artifact_id",
			coords: &MavenCoordinates{
				GroupID:    "io.zonky.test.postgres",
				ArtifactID: "embedded-postgres-binaries-{{.os}}-{{.arch}}",
				Packaging:  "jar",
			},
			version: "16.1.0",
			want: &MavenCoordinates{
				GroupID:    "io.zonky.test.postgres",
				ArtifactID: "embedded-postgres-binaries-linux-amd64",
				Packaging:  "jar",
			},
		},
		{
			name: "template classifier",
			coords: &MavenCoordinates{
				GroupID:    "org.example",
				ArtifactID: "test-lib",
				Packaging:  "jar",
				Classifier: "{{.os}}-{{.arch}}",
			},
			version: "1.0.0",
			want: &MavenCoordinates{
				GroupID:    "org.example",
				ArtifactID: "test-lib",
				Packaging:  "jar",
				Classifier: "linux-amd64",
			},
		},
		{
			name: "no templating",
			coords: &MavenCoordinates{
				GroupID:    "org.apache.commons",
				ArtifactID: "commons-lang3",
				Packaging:  "jar",
			},
			version: "3.12.0",
			want: &MavenCoordinates{
				GroupID:    "org.apache.commons",
				ArtifactID: "commons-lang3",
				Packaging:  "jar",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := manager.templateCoordinates(tt.coords, tt.version, plat)
			if err != nil {
				t.Errorf("templateCoordinates() error = %v", err)
				return
			}
			if got.GroupID != tt.want.GroupID ||
				got.ArtifactID != tt.want.ArtifactID ||
				got.Packaging != tt.want.Packaging ||
				got.Classifier != tt.want.Classifier {
				t.Errorf("templateCoordinates() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMavenManager_buildArtifactURL(t *testing.T) {
	manager := NewMavenManager()

	tests := []struct {
		name       string
		repository string
		coords     *MavenCoordinates
		want       string
	}{
		{
			name:       "basic artifact",
			repository: "https://repo1.maven.org/maven2",
			coords: &MavenCoordinates{
				GroupID:    "org.apache.commons",
				ArtifactID: "commons-lang3",
				Version:    "3.12.0",
				Packaging:  "jar",
			},
			want: "https://repo1.maven.org/maven2/org/apache/commons/commons-lang3/3.12.0/commons-lang3-3.12.0.jar",
		},
		{
			name:       "artifact with classifier",
			repository: "https://repo1.maven.org/maven2",
			coords: &MavenCoordinates{
				GroupID:    "org.apache.commons",
				ArtifactID: "commons-lang3",
				Version:    "3.12.0",
				Packaging:  "jar",
				Classifier: "sources",
			},
			want: "https://repo1.maven.org/maven2/org/apache/commons/commons-lang3/3.12.0/commons-lang3-3.12.0-sources.jar",
		},
		{
			name:       "postgres embedded",
			repository: "https://repo1.maven.org/maven2",
			coords: &MavenCoordinates{
				GroupID:    "io.zonky.test.postgres",
				ArtifactID: "embedded-postgres-binaries-linux-amd64",
				Version:    "16.1.0",
				Packaging:  "jar",
			},
			want: "https://repo1.maven.org/maven2/io/zonky/test/postgres/embedded-postgres-binaries-linux-amd64/16.1.0/embedded-postgres-binaries-linux-amd64-16.1.0.jar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.buildArtifactURL(tt.repository, tt.coords)
			if got != tt.want {
				t.Errorf("buildArtifactURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMavenManager_buildMetadataURL(t *testing.T) {
	manager := NewMavenManager()

	tests := []struct {
		name       string
		repository string
		coords     *MavenCoordinates
		want       string
	}{
		{
			name:       "basic metadata",
			repository: "https://repo1.maven.org/maven2",
			coords: &MavenCoordinates{
				GroupID:    "org.apache.commons",
				ArtifactID: "commons-lang3",
			},
			want: "https://repo1.maven.org/maven2/org/apache/commons/commons-lang3/maven-metadata.xml",
		},
		{
			name:       "nested group id",
			repository: "https://repo1.maven.org/maven2",
			coords: &MavenCoordinates{
				GroupID:    "io.zonky.test.postgres",
				ArtifactID: "embedded-postgres-binaries-linux-amd64",
			},
			want: "https://repo1.maven.org/maven2/io/zonky/test/postgres/embedded-postgres-binaries-linux-amd64/maven-metadata.xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.buildMetadataURL(tt.repository, tt.coords)
			if got != tt.want {
				t.Errorf("buildMetadataURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMavenManager_DiscoverVersions(t *testing.T) {
	// Mock Maven metadata XML
	metadataXML := `<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>org.apache.commons</groupId>
  <artifactId>commons-lang3</artifactId>
  <versioning>
    <latest>3.14.0</latest>
    <release>3.14.0</release>
    <versions>
      <version>3.12.0</version>
      <version>3.13.0</version>
      <version>3.14.0</version>
      <version>3.15.0-SNAPSHOT</version>
    </versions>
    <lastUpdated>20240315120000</lastUpdated>
  </versioning>
</metadata>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "maven-metadata.xml") {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(metadataXML))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	manager := NewMavenManager()
	pkg := types.Package{
		Name: "commons-lang3",
		Extra: map[string]interface{}{
			"group_id":    "org.apache.commons",
			"artifact_id": "commons-lang3",
			"repository":  server.URL,
		},
	}

	ctx := context.Background()
	plat := platform.Platform{OS: "linux", Arch: "amd64"}
	versions, err := manager.DiscoverVersions(ctx, pkg, plat, 0)
	if err != nil {
		t.Fatalf("DiscoverVersions() error = %v", err)
	}

	if len(versions) != 4 {
		t.Errorf("Expected 4 versions, got %d", len(versions))
	}

	// Check that versions are sorted (newest first)
	expectedOrder := []string{"3.15.0-SNAPSHOT", "3.14.0", "3.13.0", "3.12.0"}
	for i, expected := range expectedOrder {
		if i >= len(versions) {
			t.Errorf("Missing version at index %d", i)
			continue
		}
		if !strings.Contains(versions[i].Tag, expected) {
			t.Errorf("Expected version[%d] to contain '%s', got '%s'", i, expected, versions[i].Tag)
		}
	}

	// Check prerelease flag
	if !versions[0].Prerelease {
		t.Errorf("Expected first version (snapshot) to be marked as prerelease")
	}
	if versions[1].Prerelease {
		t.Errorf("Expected second version to not be marked as prerelease")
	}
}

func TestMavenManager_Resolve(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			// Simulate artifact exists
			if strings.Contains(r.URL.Path, "commons-lang3-3.12.0.jar") {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer server.Close()

	manager := NewMavenManager()
	pkg := types.Package{
		Name: "commons-lang3",
		Extra: map[string]interface{}{
			"group_id":    "org.apache.commons",
			"artifact_id": "commons-lang3",
			"packaging":   "jar",
			"repository":  server.URL,
		},
	}

	ctx := context.Background()
	plat := platform.Platform{OS: "linux", Arch: "amd64"}

	t.Run("successful resolution", func(t *testing.T) {
		resolution, err := manager.Resolve(ctx, pkg, "3.12.0", plat)
		if err != nil {
			t.Fatalf("Resolve() error = %v", err)
		}

		expectedURL := server.URL + "/org/apache/commons/commons-lang3/3.12.0/commons-lang3-3.12.0.jar"
		if resolution.DownloadURL != expectedURL {
			t.Errorf("Expected download URL '%s', got '%s'", expectedURL, resolution.DownloadURL)
		}

		expectedChecksumURL := expectedURL + ".sha1"
		if resolution.ChecksumURL != expectedChecksumURL {
			t.Errorf("Expected checksum URL '%s', got '%s'", expectedChecksumURL, resolution.ChecksumURL)
		}

		if !resolution.IsArchive {
			t.Errorf("Expected JAR to be marked as archive")
		}
	})

	t.Run("version not found", func(t *testing.T) {
		_, err := manager.Resolve(ctx, pkg, "99.99.99", plat)
		if err == nil {
			t.Errorf("Expected error for non-existent version")
		}
		if !strings.Contains(err.Error(), "99.99.99") {
			t.Errorf("Expected error to mention the version, got: %v", err)
		}
	})
}

func TestMavenManager_suggestClosestVersion(t *testing.T) {
	availableVersions := []types.Version{
		{Version: "3.14.0", Tag: "3.14.0", Prerelease: false},
		{Version: "3.13.0", Tag: "3.13.0", Prerelease: false},
		{Version: "3.12.0", Tag: "3.12.0", Prerelease: false},
		{Version: "3.15.0-SNAPSHOT", Tag: "3.15.0-SNAPSHOT", Prerelease: true},
	}

	tests := []struct {
		name             string
		requestedVersion string
		want             string
	}{
		{
			name:             "close patch version",
			requestedVersion: "3.12.1",
			want:             "3.12.0",
		},
		{
			name:             "close minor version",
			requestedVersion: "3.11.0",
			want:             "3.12.0",
		},
		{
			name:             "much newer version",
			requestedVersion: "4.0.0",
			want:             "3.14.0", // latest stable
		},
		{
			name:             "invalid semver",
			requestedVersion: "invalid",
			want:             "3.14.0", // latest stable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.SuggestClosestVersion(tt.requestedVersion, availableVersions)
			if got != tt.want {
				t.Errorf("suggestClosestVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}
