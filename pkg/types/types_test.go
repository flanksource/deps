package types

import (
	"testing"
)

func TestPackageFolderName(t *testing.T) {
	tests := []struct {
		name            string
		pkg             Package
		version         string
		expectedFolder  string
	}{
		{
			name:           "default uses package name only",
			pkg:            Package{Name: "go"},
			version:        "1.25.8",
			expectedFolder: "go",
		},
		{
			name:           "versioned folder concatenates name and version",
			pkg:            Package{Name: "go", VersionedFolder: true},
			version:        "1.25.8",
			expectedFolder: "go1.25.8",
		},
		{
			name:           "versioned folder with empty version falls back to name",
			pkg:            Package{Name: "go", VersionedFolder: true},
			version:        "",
			expectedFolder: "go",
		},
		{
			name:           "versioned folder with v-prefixed version",
			pkg:            Package{Name: "node", VersionedFolder: true},
			version:        "v22.3.0",
			expectedFolder: "nodev22.3.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.pkg.FolderName(tt.version)
			if got != tt.expectedFolder {
				t.Errorf("FolderName(%q) = %q, want %q", tt.version, got, tt.expectedFolder)
			}
		})
	}
}
