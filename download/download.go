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

// Download downloads a file with optional configuration
func Download(url, dest string, t *task.Task, opts ...DownloadOption) error {
	// Parse options
	config := &downloadConfig{}
	for _, opt := range opts {
		opt(config)
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
		t.Success()
	}

	return nil
}

// DownloadAndVerifyWithChecksumURL downloads a file and verifies against a checksum from a URL
// The checksumURL can be the download URL + suffix (e.g., ".sha256", ".sha256sum")
func DownloadAndVerifyWithChecksumURL(url, dest, checksumURL string, t *task.Task) error {
	return DownloadAndVerifyWithChecksumFiles(url, dest, []string{checksumURL}, "", t)
}

// DownloadAndVerifyWithChecksumFiles downloads a file and verifies against checksums from multiple files
// using an optional CEL expression to extract the correct checksum
func DownloadAndVerifyWithChecksumFiles(url, dest string, checksumURLs []string, checksumExpr string, t *task.Task) error {
	if len(checksumURLs) == 0 {
		return fmt.Errorf("no checksum URLs provided")
	}

	// 1. Download all checksum files
	checksumContents := make(map[string]string)

	for _, checksumURL := range checksumURLs {
		if t != nil {
			t.Infof("Fetching checksum from %s", checksumURL)
		}

		// Create HTTP client with redirect logging for checksum download
		client := createHTTPClient(t)
		resp, err := client.Get(checksumURL)
		if err != nil {
			return fmt.Errorf("failed to download checksum file %s: %w", checksumURL, err)
		}
		defer resp.Body.Close()

		// Log final checksum URL if different from original (redirected)
		if t != nil && resp.Request.URL.String() != checksumURL {
			t.Debugf("Checksum URL after redirects: %s", resp.Request.URL.String())
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("checksum file not found at %s: status %d", checksumURL, resp.StatusCode)
		}

		// Read checksum file content
		content, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read checksum file %s: %w", checksumURL, err)
		}

		// Use filename without extension as key for CEL variables
		filename := filepath.Base(checksumURL)
		if ext := filepath.Ext(filename); ext != "" {
			filename = strings.TrimSuffix(filename, ext)
		}
		checksumContents[filename] = string(content)
	}

	// 2. Extract checksum using CEL expression or fallback parsing
	var checksumValue string
	var checksumType checksum.HashType
	var err error

	if checksumExpr != "" {
		checksumValue, checksumType, err = checksum.EvaluateCELExpression(checksumContents, url, checksumExpr)
		if err != nil {
			return err
		}
	} else {
		// Fallback to standard parsing with the first checksum file
		content := checksumContents[getFirstChecksumKey(checksumContents)]
		checksumValue, checksumType, err = checksum.ParseChecksumFile(content, url)
		if err != nil {
			return err
		}
	}

	if err != nil {
		return fmt.Errorf("failed to extract checksum: %w", err)
	}

	// Log the found checksum
	if t != nil {
		filename := filepath.Base(url)
		checksumDisplay := checksumValue
		if len(checksumDisplay) > 8 {
			checksumDisplay = checksumDisplay[:8] + "..."
		}

		// Extract source file names from URLs
		var sourceNames []string
		for _, checksumURL := range checksumURLs {
			sourceNames = append(sourceNames, filepath.Base(checksumURL))
		}
		sourcesDisplay := strings.Join(sourceNames, ", ")

		t.Infof("Found %s:%s checksum for %s (from %s)", checksumType, checksumDisplay, filename, sourcesDisplay)
	}

	// 3. Download the actual file with verification
	return Download(url, dest, t,
		WithChecksum(checksumValue),
		WithChecksumType(string(checksumType)),
		WithChecksumSource(strings.Join(checksumURLs, ",")))
}

// getFirstChecksumKey returns the first key from the checksumContents map
func getFirstChecksumKey(checksumContents map[string]string) string {
	for key := range checksumContents {
		return key
	}
	return ""
}

// getChecksumFileNames returns a slice of the checksum file names for debugging
func getChecksumFileNames(checksumContents map[string]string) []string {
	var names []string
	for name := range checksumContents {
		names = append(names, name)
	}
	return names
}

// DownloadWithProgress downloads a file with progress tracking
func DownloadWithProgress(url, dest string, t *task.Task) error {
	return Download(url, dest, t)
}

// DownloadWithChecksum downloads and verifies checksum
func DownloadWithChecksum(url, dest, expectedChecksum string, t *task.Task) error {
	return Download(url, dest, t, WithChecksum(expectedChecksum))
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
