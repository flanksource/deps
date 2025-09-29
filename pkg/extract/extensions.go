package extract

import (
	"path/filepath"
	"strings"
)

// GetExtension returns the file extension from a URL, handling archives and installers
func GetExtension(url string) string {
	// Remove query parameters from URLs
	if idx := strings.Index(url, "?"); idx != -1 {
		url = url[:idx]
	}

	lower := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(lower, ".tgz"):
		return ".tgz"
	case strings.HasSuffix(lower, ".tar.xz"):
		return ".tar.xz"
	case strings.HasSuffix(lower, ".tar.bz2"):
		return ".tar.bz2"
	case strings.HasSuffix(lower, ".tar"):
		return ".tar"
	case strings.HasSuffix(lower, ".txz"):
		return ".txz"
	case strings.HasSuffix(lower, ".zip"):
		return ".zip"
	case strings.HasSuffix(lower, ".jar"):
		return ".jar"
	case strings.HasSuffix(lower, ".pkg"):
		return ".pkg"
	case strings.HasSuffix(lower, ".msi"):
		return ".msi"
	default:
		// Fall back to filepath.Ext for other extensions
		return filepath.Ext(url)
	}
}

// IsArchive returns true if the file appears to be an archive based on its extension
func IsArchive(path string) bool {
	lower := strings.ToLower(path)
	extensions := []string{".tar", ".tar.gz", ".tgz", ".tar.xz", ".txz", ".zip", ".jar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".zip", ".jar", ".war"}
	for _, ext := range extensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
