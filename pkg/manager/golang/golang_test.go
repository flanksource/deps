package golang

import (
	"testing"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

func TestGoManager_Name(t *testing.T) {
	manager := NewGoManager()
	if manager.Name() != "go" {
		t.Errorf("expected manager name 'go', got '%s'", manager.Name())
	}
}

func TestGoManager_GetImportPath(t *testing.T) {
	tests := []struct {
		name        string
		pkg         types.Package
		want        string
		expectError bool
	}{
		{
			name: "valid import path",
			pkg: types.Package{
				Name: "ginkgo",
				Extra: map[string]interface{}{
					"import_path": "github.com/onsi/ginkgo/v2/ginkgo",
				},
			},
			want:        "github.com/onsi/ginkgo/v2/ginkgo",
			expectError: false,
		},
		{
			name: "missing extra",
			pkg: types.Package{
				Name: "ginkgo",
			},
			want:        "",
			expectError: true,
		},
		{
			name: "missing import_path",
			pkg: types.Package{
				Name:  "ginkgo",
				Extra: map[string]interface{}{},
			},
			want:        "",
			expectError: true,
		},
	}

	manager := NewGoManager()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := manager.getImportPath(tt.pkg)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}

			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("getImportPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGoManager_GetBinaryName(t *testing.T) {
	tests := []struct {
		name string
		pkg  types.Package
		want string
	}{
		{
			name: "extract from import path",
			pkg: types.Package{
				Name: "ginkgo",
				Extra: map[string]interface{}{
					"import_path": "github.com/onsi/ginkgo/v2/ginkgo",
				},
			},
			want: "ginkgo",
		},
		{
			name: "fallback to package name",
			pkg: types.Package{
				Name: "mytool",
			},
			want: "mytool",
		},
	}

	manager := NewGoManager()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.getBinaryName(tt.pkg)
			if got != tt.want {
				t.Errorf("getBinaryName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGoManager_Resolve(t *testing.T) {
	manager := NewGoManager()

	pkg := types.Package{
		Name: "ginkgo",
		Repo: "onsi/ginkgo",
		Extra: map[string]interface{}{
			"import_path": "github.com/onsi/ginkgo/v2/ginkgo",
		},
	}

	plat := platform.Platform{
		OS:   "darwin",
		Arch: "arm64",
	}

	resolution, err := manager.Resolve(nil, pkg, "v2.23.2", plat)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// Go packages don't have a download URL - Install() handles everything
	if resolution.DownloadURL != "" {
		t.Errorf("Resolve() DownloadURL = %v, want empty string", resolution.DownloadURL)
	}

	if resolution.Version != "v2.23.2" {
		t.Errorf("Resolve() Version = %v, want v2.23.2", resolution.Version)
	}

	if resolution.IsArchive {
		t.Error("Resolve() IsArchive should be false for Go packages")
	}

	// Verify package information is preserved
	if resolution.Package.Name != "ginkgo" {
		t.Errorf("Resolve() Package.Name = %v, want ginkgo", resolution.Package.Name)
	}
}
