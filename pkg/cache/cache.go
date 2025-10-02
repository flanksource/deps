package cache

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/deps/pkg/utils"
)

// GetCachePath generates a cache path for a URL and filename
// Format: {cacheDir}/{url-hash}/{filename}
func GetCachePath(cacheDir, url, filename string) string {
	if cacheDir == "" {
		return ""
	}

	// Create a hash of the URL to avoid path length issues
	urlHash := hashURL(url)
	return filepath.Join(cacheDir, urlHash, filename)
}

// IsCached checks if a file exists in the cache
// Returns the cache path and true if cached, empty string and false otherwise
func IsCached(cacheDir, url, filename string) (string, bool) {
	if cacheDir == "" {
		return "", false
	}

	cachePath := GetCachePath(cacheDir, url, filename)
	if _, err := os.Stat(cachePath); err == nil {
		return cachePath, true
	}
	return "", false
}

// SaveToCache copies a file to the cache
func SaveToCache(cacheDir, url, sourcePath string) error {
	if cacheDir == "" {
		return nil // Caching disabled
	}

	filename := filepath.Base(sourcePath)
	cachePath := GetCachePath(cacheDir, url, filename)

	// Create cache directory
	cacheSubDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheSubDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Copy file to cache
	if err := utils.CopyFile(sourcePath, cachePath); err != nil {
		return fmt.Errorf("failed to copy to cache: %w", err)
	}

	return nil
}

// hashURL creates a short hash of a URL for directory naming
func hashURL(url string) string {
	// Normalize URL by removing protocol and trailing slashes
	normalized := strings.TrimPrefix(url, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	normalized = strings.TrimSuffix(normalized, "/")

	// Create SHA256 hash and take first 16 chars for readability
	hash := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", hash[:8])
}

// CopyFromCache copies a file from cache to destination
func CopyFromCache(cachePath, dest string) error {
	// Create destination directory if needed
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Copy from cache
	if err := utils.CopyFile(cachePath, dest); err != nil {
		return fmt.Errorf("failed to copy from cache: %w", err)
	}

	return nil
}

// ValidateCachedFile checks if a cached file matches the expected checksum
// Returns true if the file is valid, false otherwise
func ValidateCachedFile(cachePath, expectedChecksum string, hasher func() (string, error)) (bool, error) {
	if expectedChecksum == "" {
		// No checksum to validate against, assume valid
		return true, nil
	}

	// Open cached file
	f, err := os.Open(cachePath)
	if err != nil {
		return false, fmt.Errorf("failed to open cached file: %w", err)
	}
	defer f.Close()

	// Compute checksum
	actualChecksum, err := hasher()
	if err != nil {
		return false, fmt.Errorf("failed to compute checksum: %w", err)
	}

	return actualChecksum == expectedChecksum, nil
}

// ComputeFileChecksum computes a checksum of a file
func ComputeFileChecksum(path string, hasher io.Writer) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("failed to compute checksum: %w", err)
	}

	return "", nil // Caller should extract checksum from hasher
}
