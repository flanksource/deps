//go:build darwin

package pipeline

import (
	"runtime"
	"testing"
)

func TestParseOtoolOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []DylibRef
	}{
		{
			name: "typical postgrest output with broken ref",
			output: `/tmp/postgrest:
	/opt/homebrew/opt/libpq/lib/libpq.5.dylib (compatibility version 5.0.0, current version 5.17.0)
	/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1336.0.0)
	/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation (compatibility version 150.0.0, current version 2503.1.0)
`,
			expected: []DylibRef{
				{Path: "/opt/homebrew/opt/libpq/lib/libpq.5.dylib", Found: false},
			},
		},
		{
			name:     "empty output",
			output:   "",
			expected: nil,
		},
		{
			name: "skip @rpath references",
			output: `test:
	@rpath/libfoo.dylib (compatibility version 1.0.0, current version 1.0.0)
	/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1336.0.0)
`,
			expected: nil,
		},
		{
			name: "multiple broken refs",
			output: `binary:
	/nonexistent/path/libpq.5.dylib (compatibility version 5.0.0, current version 5.17.0)
	/also/missing/libssl.3.dylib (compatibility version 3.0.0, current version 3.1.0)
	/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1336.0.0)
`,
			expected: []DylibRef{
				{Path: "/nonexistent/path/libpq.5.dylib", Found: false},
				{Path: "/also/missing/libssl.3.dylib", Found: false},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseOtoolOutput(tc.output, "")
			if len(result) != len(tc.expected) {
				t.Fatalf("expected %d refs, got %d: %+v", len(tc.expected), len(result), result)
			}
			for i, ref := range result {
				if ref.Path != tc.expected[i].Path {
					t.Errorf("ref[%d].Path = %q, want %q", i, ref.Path, tc.expected[i].Path)
				}
				if ref.Found != tc.expected[i].Found {
					t.Errorf("ref[%d].Found = %v, want %v", i, ref.Found, tc.expected[i].Found)
				}
			}
		})
	}
}

func TestFindLibrary(t *testing.T) {
	result, _, _ := findLibrary("libpq.5.dylib", "")
	if result == "" {
		t.Skip("libpq.5.dylib not found on this system")
	}
	t.Logf("found libpq (any arch) at: %s", result)
}

func TestFindLibraryWithArch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("architecture filtering only applies to darwin")
	}
	arch := map[string]string{"arm64": "arm64", "amd64": "x86_64"}[runtime.GOARCH]
	if arch == "" {
		t.Skipf("unknown GOARCH: %s", runtime.GOARCH)
	}

	result, _, _ := findLibrary("libpq.5.dylib", arch)
	if result == "" {
		t.Skipf("no %s libpq.5.dylib found on this system", arch)
	}
	t.Logf("found %s libpq at: %s", arch, result)

	// Verify the library is actually the right architecture
	libArch := detectArch(result)
	if libArch != arch {
		t.Errorf("findLibrary returned %s with arch %q, want %q", result, libArch, arch)
	}
}

func TestFindLibraryArchMismatch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("architecture mismatch detection only applies to darwin")
	}
	// Request the opposite architecture to trigger a mismatch
	oppositeArch := map[string]string{"arm64": "x86_64", "amd64": "arm64"}[runtime.GOARCH]
	if oppositeArch == "" {
		t.Skipf("unknown GOARCH: %s", runtime.GOARCH)
	}

	path, mismatchPath, mismatchArch := findLibrary("libpq.5.dylib", oppositeArch)
	if path != "" {
		t.Skipf("found a %s libpq — no mismatch to test", oppositeArch)
	}
	if mismatchPath == "" {
		t.Skip("no libpq.5.dylib found at all on this system")
	}
	t.Logf("mismatch: found %s at %s (wanted %s)", mismatchArch, mismatchPath, oppositeArch)
	if mismatchArch == oppositeArch {
		t.Errorf("mismatchArch should differ from requested arch %s", oppositeArch)
	}
}

func TestMachoArchToGoArch(t *testing.T) {
	tests := []struct{ input, want string }{
		{"x86_64", "amd64"},
		{"arm64", "arm64"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := machoArchToGoArch(tc.input); got != tc.want {
			t.Errorf("machoArchToGoArch(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDetectArch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("detectArch uses macOS file command")
	}
	// Use a known existing library instead of system cache dylibs
	lib, _, _ := findLibrary("libpq.5.dylib", "")
	if lib == "" {
		t.Skip("no libpq.5.dylib found to test arch detection")
	}
	arch := detectArch(lib)
	t.Logf("%s arch: %s", lib, arch)
	if arch == "" {
		t.Errorf("expected non-empty arch for %s", lib)
	}
}
