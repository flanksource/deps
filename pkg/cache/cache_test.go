package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetCachePath(t *testing.T) {
	tests := []struct {
		name     string
		cacheDir string
		url      string
		filename string
		wantPath bool
	}{
		{
			name:     "valid cache path",
			cacheDir: "/tmp/cache",
			url:      "https://example.com/file.tar.gz",
			filename: "file.tar.gz",
			wantPath: true,
		},
		{
			name:     "empty cache dir",
			cacheDir: "",
			url:      "https://example.com/file.tar.gz",
			filename: "file.tar.gz",
			wantPath: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCachePath(tt.cacheDir, tt.url, tt.filename)
			if tt.wantPath && got == "" {
				t.Errorf("GetCachePath() = %q, want non-empty path", got)
			}
			if !tt.wantPath && got != "" {
				t.Errorf("GetCachePath() = %q, want empty path", got)
			}
			if tt.wantPath && !filepath.IsAbs(got) {
				t.Errorf("GetCachePath() = %q, want absolute path", got)
			}
		})
	}
}

func TestIsCached(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a cached file
	url := "https://example.com/test.tar.gz"
	filename := "test.tar.gz"
	cachePath := GetCachePath(tmpDir, url, filename)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create cache file: %v", err)
	}

	tests := []struct {
		name       string
		cacheDir   string
		url        string
		filename   string
		wantCached bool
	}{
		{
			name:       "file exists in cache",
			cacheDir:   tmpDir,
			url:        url,
			filename:   filename,
			wantCached: true,
		},
		{
			name:       "file not in cache",
			cacheDir:   tmpDir,
			url:        "https://example.com/nonexistent.tar.gz",
			filename:   "nonexistent.tar.gz",
			wantCached: false,
		},
		{
			name:       "cache disabled",
			cacheDir:   "",
			url:        url,
			filename:   filename,
			wantCached: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotCached := IsCached(tt.cacheDir, tt.url, tt.filename)
			if gotCached != tt.wantCached {
				t.Errorf("IsCached() cached = %v, want %v", gotCached, tt.wantCached)
			}
			if tt.wantCached && gotPath == "" {
				t.Errorf("IsCached() path = %q, want non-empty", gotPath)
			}
			if !tt.wantCached && gotPath != "" {
				t.Errorf("IsCached() path = %q, want empty", gotPath)
			}
		})
	}
}

func TestSaveToCache(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "source.txt")
	content := []byte("test content for caching")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	tests := []struct {
		name      string
		cacheDir  string
		url       string
		srcPath   string
		wantError bool
	}{
		{
			name:      "save to cache successfully",
			cacheDir:  tmpDir,
			url:       "https://example.com/test.tar.gz",
			srcPath:   srcFile,
			wantError: false,
		},
		{
			name:      "cache disabled",
			cacheDir:  "",
			url:       "https://example.com/test.tar.gz",
			srcPath:   srcFile,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SaveToCache(tt.cacheDir, tt.url, tt.srcPath)
			if (err != nil) != tt.wantError {
				t.Errorf("SaveToCache() error = %v, wantError %v", err, tt.wantError)
			}

			if !tt.wantError && tt.cacheDir != "" {
				// Verify file was saved
				filename := filepath.Base(tt.srcPath)
				cachePath := GetCachePath(tt.cacheDir, tt.url, filename)
				if _, err := os.Stat(cachePath); os.IsNotExist(err) {
					t.Errorf("SaveToCache() file not saved to %q", cachePath)
				}

				// Verify content matches
				cachedContent, err := os.ReadFile(cachePath)
				if err != nil {
					t.Errorf("Failed to read cached file: %v", err)
				}
				if string(cachedContent) != string(content) {
					t.Errorf("Cached content = %q, want %q", cachedContent, content)
				}
			}
		})
	}
}

func TestCopyFromCache(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "cached.txt")
	content := []byte("cached content")
	if err := os.WriteFile(cacheFile, content, 0644); err != nil {
		t.Fatalf("Failed to create cache file: %v", err)
	}

	destFile := filepath.Join(tmpDir, "dest", "output.txt")

	err := CopyFromCache(cacheFile, destFile)
	if err != nil {
		t.Errorf("CopyFromCache() error = %v", err)
	}

	// Verify file was copied
	if _, err := os.Stat(destFile); os.IsNotExist(err) {
		t.Errorf("CopyFromCache() file not copied to %q", destFile)
	}

	// Verify content matches
	destContent, err := os.ReadFile(destFile)
	if err != nil {
		t.Errorf("Failed to read destination file: %v", err)
	}
	if string(destContent) != string(content) {
		t.Errorf("Destination content = %q, want %q", destContent, content)
	}
}

func TestHashURL(t *testing.T) {
	tests := []struct {
		url1 string
		url2 string
		want bool // true if hashes should match
	}{
		{
			url1: "https://example.com/file.tar.gz",
			url2: "https://example.com/file.tar.gz",
			want: true,
		},
		{
			url1: "https://example.com/file.tar.gz",
			url2: "http://example.com/file.tar.gz",
			want: true, // Protocol should be normalized
		},
		{
			url1: "https://example.com/file.tar.gz",
			url2: "https://example.com/other.tar.gz",
			want: false,
		},
	}

	for _, tt := range tests {
		hash1 := hashURL(tt.url1)
		hash2 := hashURL(tt.url2)
		matches := hash1 == hash2

		if matches != tt.want {
			t.Errorf("hashURL(%q) vs hashURL(%q): got match=%v, want match=%v",
				tt.url1, tt.url2, matches, tt.want)
		}

		// Verify hash length is reasonable
		if len(hash1) != 16 {
			t.Errorf("hashURL() length = %d, want 16", len(hash1))
		}
	}
}
