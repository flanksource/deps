package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/clicky/task"
)

// RelativePath converts an absolute path to a relative path from the current working directory
func RelativePath(absPath string) string {
	if absPath == "" {
		return ""
	}

	cwd, err := os.Getwd()
	if err != nil {
		// If we can't get working directory, just return the basename
		return filepath.Base(absPath)
	}

	relPath, err := filepath.Rel(cwd, absPath)
	if err != nil {
		// If we can't make it relative, return basename
		return filepath.Base(absPath)
	}

	// If relative path is longer than original, use basename
	if len(relPath) > len(absPath) {
		return filepath.Base(absPath)
	}

	return relPath
}

// LogPath returns a clean path for logging (relative if shorter, basename otherwise)
func LogPath(path string) string {
	if path == "" {
		return ""
	}

	// Convert to absolute first
	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Base(path)
	}

	return RelativePath(absPath)
}

// FormatFileInfo returns a formatted string with file size and permissions
func FormatFileInfo(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return filepath.Base(path)
	}

	size := FormatBytes(info.Size())
	mode := info.Mode()

	if info.IsDir() {
		return fmt.Sprintf("%s (dir)", filepath.Base(path))
	}

	return fmt.Sprintf("%s (%s, %o)", filepath.Base(path), size, mode&0777)
}

// FormatBytes formats bytes into human-readable format
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// ShortenURL shortens a URL for logging by removing protocol and showing only domain + path
func ShortenURL(url string) string {
	if url == "" {
		return ""
	}

	// Remove protocol
	if strings.HasPrefix(url, "https://") {
		url = url[8:]
	} else if strings.HasPrefix(url, "http://") {
		url = url[7:]
	}

	// If URL is still very long, truncate middle part
	if len(url) > 60 {
		parts := strings.Split(url, "/")
		if len(parts) > 2 {
			domain := parts[0]
			filename := parts[len(parts)-1]
			return fmt.Sprintf("%s/.../%s", domain, filename)
		}
	}

	return url
}

// LogOperation executes a function and logs start/completion in a unified way
func LogOperation(t *task.Task, operation, target string, fn func() error) error {
	if t == nil {
		return fn()
	}

	// Set initial description
	t.SetDescription(fmt.Sprintf("%s %s...", operation, target))

	start := time.Now()
	err := fn()
	duration := time.Since(start)

	if err != nil {
		t.Errorf("❌ %s failed: %v", operation, err)
		return err
	}

	// Show completion with timing for longer operations
	if duration > 500*time.Millisecond {
		t.Infof("✅ %s completed (%v)", operation, duration.Round(10*time.Millisecond))
	} else {
		t.Infof("✅ %s completed", operation)
	}

	return nil
}

// LogFileFound logs discovery of a file with context
func LogFileFound(t *task.Task, path, context string) {
	if t == nil {
		return
	}

	info := FormatFileInfo(path)
	logPath := LogPath(path)

	if context != "" {
		t.V(4).Infof("Found %s at %s (%s)", context, logPath, info)
	} else {
		t.V(4).Infof("Found %s", info)
	}
}

// LogDownloadStart logs the start of a download with clean formatting
func LogDownloadStart(t *task.Task, url, dest string) {
	if t == nil {
		return
	}

	shortURL := ShortenURL(url)
	t.Infof("Downloading from %s", shortURL)
	t.SetDescription(fmt.Sprintf("Downloading %s", filepath.Base(dest)))
}

// LogChecksumFetch logs checksum fetching for one or more URLs
func LogChecksumFetch(t *task.Task, urls []string) {
	if t == nil || len(urls) == 0 {
		return
	}

	if len(urls) == 1 {
		t.V(3).Infof("Fetching checksum from %s", ShortenURL(urls[0]))
	} else {
		t.V(3).Infof("Fetching checksums from %d sources", len(urls))
	}
}

// LogBinarySearch logs binary search progress in a consolidated way
func LogBinarySearch(t *task.Task, searchDir, binaryName string, found bool, foundPath string) {
	if t == nil {
		return
	}

	searchPath := LogPath(searchDir)

	if found {
		t.V(4).Infof("Binary search in %s → found %s", searchPath, LogPath(foundPath))
	} else {
		t.V(4).Infof("Binary search in %s → %s not found", searchPath, binaryName)
	}
}

// LogExtraction logs extraction operations with file count
func LogExtraction(t *task.Task, archivePath, extractDir string, fileCount int) {
	if t == nil {
		return
	}

	archiveName := filepath.Base(archivePath)
	extractPath := LogPath(extractDir)

	if fileCount > 0 {
		t.Infof("Extracted %s (%d files) to %s", archiveName, fileCount, extractPath)
	} else {
		t.Infof("Extracting %s to %s", archiveName, extractPath)
	}
}
