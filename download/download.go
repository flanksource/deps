package download

import (
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/checksum"
)

// DownloadResult holds information about a completed download
type DownloadResult struct {
	ChecksumUsed    string
	ChecksumType    string
	ChecksumSources []string
}

// createHTTPClient creates an HTTP client with redirect logging
func createHTTPClient(t *task.Task) *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 10 redirects (Go's default)
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects (limit: 10)")
			}

			// Log redirect chain
			if t != nil && len(via) > 0 {
				from := via[len(via)-1].URL.String()
				to := req.URL.String()
				t.Debugf("Following redirect: %s → %s", from, to)
			}

			return nil
		},
	}
}

// DownloadOption is a functional option for configuring downloads
type DownloadOption func(*downloadConfig)

// downloadConfig holds configuration for downloads
type downloadConfig struct {
	expectedChecksum string
	checksumType     string // "sha256", "sha512", "md5", etc.
	checksumSource   string // URL or filename where checksum came from
	skipProgress     bool
	checksumURL      string   // Single checksum URL to fetch
	checksumURLs     []string // Multiple checksum URLs to fetch
	checksumExpr     string   // CEL expression for extracting checksum from files
	simpleMode       bool     // No task/progress support
}

// WithChecksum sets the expected checksum for verification
func WithChecksum(checksum string) DownloadOption {
	return func(c *downloadConfig) {
		c.expectedChecksum = strings.TrimSpace(checksum)
	}
}

// WithoutProgress disables progress tracking even if task is provided
func WithoutProgress() DownloadOption {
	return func(c *downloadConfig) {
		c.skipProgress = true
	}
}

// WithChecksumType sets the checksum type (sha256, sha512, md5, etc.)
func WithChecksumType(checksumType string) DownloadOption {
	return func(c *downloadConfig) {
		c.checksumType = strings.ToLower(strings.TrimSpace(checksumType))
	}
}

// WithChecksumSource sets where the checksum came from (URL or filename)
func WithChecksumSource(source string) DownloadOption {
	return func(c *downloadConfig) {
		c.checksumSource = strings.TrimSpace(source)
	}
}

// WithChecksumURL sets a single checksum URL to fetch and verify against
func WithChecksumURL(url string) DownloadOption {
	return func(c *downloadConfig) {
		c.checksumURL = strings.TrimSpace(url)
	}
}

// WithChecksumURLs sets multiple checksum URLs and optional CEL expression
func WithChecksumURLs(urls []string, expr string) DownloadOption {
	return func(c *downloadConfig) {
		c.checksumURLs = urls
		c.checksumExpr = strings.TrimSpace(expr)
	}
}

// WithSimpleMode disables task/progress support for simple downloads
func WithSimpleMode() DownloadOption {
	return func(c *downloadConfig) {
		c.simpleMode = true
	}
}

// ProgressReader wraps an io.Reader and reports progress
type ProgressReader struct {
	io.Reader
	total      int64
	current    int64
	task       *task.Task
	depName    string
	lastUpdate time.Time
	startTime  time.Time
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	pr.current += int64(n)

	// Update progress at most once per 100ms to avoid excessive updates
	now := time.Now()
	if now.Sub(pr.lastUpdate) >= 100*time.Millisecond {
		if pr.total > 0 {
			pr.task.SetProgress(int(pr.current), int(pr.total))

			// Calculate speed
			elapsed := now.Sub(pr.startTime).Seconds()
			if elapsed > 0 {
				speed := float64(pr.current) / elapsed
				remaining := pr.total - pr.current
				eta := time.Duration(float64(remaining) / speed * float64(time.Second))

				pr.task.SetDescription(fmt.Sprintf("%s/%s (%.1f MB/s, ETA: %s)",
					formatBytes(pr.current),
					formatBytes(pr.total),
					speed/1024/1024,
					formatDuration(eta)))
			}
		} else {
			pr.task.SetDescription(fmt.Sprintf("Downloaded %s", formatBytes(pr.current)))
		}
		pr.lastUpdate = now
	}

	return n, err
}

// fetchChecksumFromURL fetches checksum from a single URL
func fetchChecksumFromURL(checksumURL, downloadURL string, t *task.Task) (checksumValue, checksumType string, sources []string, err error) {
	if t != nil {
		t.Infof("Fetching checksum from %s", checksumURL)
	}

	client := createHTTPClient(t)
	resp, err := client.Get(checksumURL)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to download checksum file %s: %w", checksumURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", nil, fmt.Errorf("checksum file not found at %s: status %d", checksumURL, resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to read checksum file %s: %w", checksumURL, err)
	}

	checksumValue, checksumHashType, err := checksum.ParseChecksumFile(string(content), downloadURL)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to parse checksum: %w", err)
	}
	return checksumValue, string(checksumHashType), []string{checksumURL}, nil
}

// fetchChecksumFromMultipleURLs fetches checksums from multiple URLs with optional CEL expression
func fetchChecksumFromMultipleURLs(checksumURLs []string, checksumExpr, downloadURL string, t *task.Task) (checksumValue, checksumType string, sources []string, err error) {
	checksumContents := make(map[string]string)
	allSources := []string{}

	// Download all checksum files
	for _, checksumURL := range checksumURLs {
		if t != nil {
			t.Infof("Fetching checksum from %s", checksumURL)
		}

		client := createHTTPClient(t)
		resp, err := client.Get(checksumURL)
		if err != nil {
			return "", "", nil, fmt.Errorf("failed to download checksum file %s: %w", checksumURL, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", "", nil, fmt.Errorf("checksum file not found at %s: status %d", checksumURL, resp.StatusCode)
		}

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", "", nil, fmt.Errorf("failed to read checksum file %s: %w", checksumURL, err)
		}

		checksumContents[checksumURL] = string(content)
		allSources = append(allSources, checksumURL)
	}

	// Parse checksum using CEL expression or fallback to simple parsing
	if checksumExpr != "" {
		checksumValue, checksumHashType, err := checksum.EvaluateCELExpression(checksumContents, downloadURL, checksumExpr)
		if err != nil {
			return "", "", nil, err
		}
		return checksumValue, string(checksumHashType), allSources, nil
	} else {
		// Try to parse from all files, return first successful match
		for _, checksumURL := range checksumURLs {
			content := checksumContents[checksumURL]
			checksumValue, checksumHashType, err := checksum.ParseChecksumFile(content, downloadURL)
			if err == nil && checksumValue != "" {
				return checksumValue, string(checksumHashType), allSources, nil
			}
		}
		return "", "", nil, fmt.Errorf("no valid checksum found in any of the checksum files")
	}
}

// Download downloads a file with optional configuration
func Download(url, dest string, t *task.Task, opts ...DownloadOption) error {
	// Parse options
	config := &downloadConfig{}
	for _, opt := range opts {
		opt(config)
	}

	// Handle simple mode - disable task if configured
	if config.simpleMode {
		t = nil
	}

	// Create destination directory if it doesn't exist
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	// Create temporary file for atomic download
	tempFile := dest + ".tmp"
	out, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file %s: %w", tempFile, err)
	}
	defer func() {
		out.Close()
		// Clean up temp file if it still exists (not renamed)
		if _, err := os.Stat(tempFile); err == nil {
			os.Remove(tempFile)
		}
	}()

	if t != nil {
		t.Debugf("Downloading from %s to %s", url, dest)
	}

	// Create HTTP client with redirect logging
	client := createHTTPClient(t)

	// Get the data
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if t != nil {
		t.Debugf("Download: HTTP %d for %s (Content-Length: %d)", resp.StatusCode, url, resp.ContentLength)
	}

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d %s for %s", resp.StatusCode, resp.Status, url)
	}

	var checksumType checksum.HashType
	if config.expectedChecksum != "" && config.checksumType == "" {
		var err error

		config.expectedChecksum, checksumType, err = checksum.ParseChecksumWithType(config.expectedChecksum)
		config.checksumType = string(checksumType)
		if err != nil {
			return fmt.Errorf("invalid checksum format: %w", err)
		}
	} else if config.checksumType != "" {
		checksumType = checksum.HashType(config.checksumType)
	}

	var reader io.Reader = resp.Body
	var writer io.Writer = out
	var hasher hash.Hash

	// Only create hasher if we have a checksum to verify
	if config.expectedChecksum != "" || checksumType != "" {
		var err error
		hasher, err = checksum.CreateHasher(checksumType)
		if err != nil {
			return fmt.Errorf("failed to create hasher: %w", err)
		}
		writer = io.MultiWriter(writer, hasher)
	}

	// Add progress tracking if task provided and not disabled
	var pr *ProgressReader
	if t != nil && !config.skipProgress {
		pr = &ProgressReader{
			Reader:     resp.Body,
			total:      resp.ContentLength,
			task:       t,
			depName:    t.Name(),
			startTime:  time.Now(),
			lastUpdate: time.Now(),
		}
		reader = pr
	}

	// Download
	written, err := io.Copy(writer, reader)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}

	// Close the temp file before verification/rename
	out.Close()

	// Fetch checksum from URL if configured (takes precedence over expectedChecksum)
	if config.checksumURL != "" {
		checksumValue, checksumType, checksumSources, err := fetchChecksumFromURL(config.checksumURL, url, t)
		if err != nil {
			return fmt.Errorf("failed to fetch checksum: %w", err)
		}
		config.expectedChecksum = checksumValue
		if config.checksumType == "" {
			config.checksumType = checksumType
		}
		if config.checksumSource == "" {
			config.checksumSource = strings.Join(checksumSources, ",")
		}
	} else if len(config.checksumURLs) > 0 {
		checksumValue, checksumType, checksumSources, err := fetchChecksumFromMultipleURLs(config.checksumURLs, config.checksumExpr, url, t)
		if err != nil {
			return fmt.Errorf("failed to fetch checksum: %w", err)
		}
		config.expectedChecksum = checksumValue
		if config.checksumType == "" {
			config.checksumType = checksumType
		}
		if config.checksumSource == "" {
			config.checksumSource = strings.Join(checksumSources, ",")
		}
	}

	// Verify checksum if provided and hasher was created
	if config.expectedChecksum != "" && hasher != nil {
		filename := filepath.Base(dest)

		// Log verification start
		if t != nil {
			t.Debugf("Verifying %s integrity...", filename)
		}

		actualChecksum := fmt.Sprintf("%x", hasher.Sum(nil))
		if actualChecksum != config.expectedChecksum {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", config.expectedChecksum, actualChecksum)
		}

		// Log successful verification with prominent message
		if t != nil {
			checksumDisplay := actualChecksum
			if len(checksumDisplay) > 8 {
				checksumDisplay = checksumDisplay[:8] + "..."
			}

			if config.checksumSource != "" {
				t.Infof("✅ Checksum verified: %s:%s (from %s)",
					checksumType, checksumDisplay, config.checksumSource)
			} else {
				t.Infof("✅ Checksum verified: %s:%s",
					checksumType, checksumDisplay)
			}
		}
	}

	// Atomically move temp file to final destination
	if err := os.Rename(tempFile, dest); err != nil {
		return fmt.Errorf("failed to move temp file to destination: %w", err)
	}

	if t != nil {
		filename := filepath.Base(dest)
		name := fmt.Sprintf("%s (%s)",
			filename,
			formatBytes(written))

		t.SetDescription(name)
		// Don't call t.Success() here - let the caller control when to mark success
		// This allows post-processing operations to complete before marking task successful
	}

	return nil
}

// getChecksumFileNames returns a slice of the checksum file names for debugging
func getChecksumFileNames(checksumContents map[string]string) []string {
	var names []string
	for name := range checksumContents {
		names = append(names, name)
	}
	return names
}

// formatBytes formats bytes into human-readable format
func formatBytes(bytes int64) string {
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

// formatDuration formats duration into human-readable format
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

// SimpleDownload performs a simple HTTP download without progress tracking
func SimpleDownload(url, dest string) (*http.Response, error) {
	// Create HTTP client with redirect logging (no task for logging)
	client := createHTTPClient(nil)

	// Get response first to check status
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download from %s: %w", url, err)
	}
	defer resp.Body.Close()

	// Return response for status check
	if resp.StatusCode != http.StatusOK {
		return resp, nil // Return response even if not OK for caller to check
	}

	// Use new Download function without task (no progress)
	err = Download(url, dest, nil, WithoutProgress())
	return resp, err
}
