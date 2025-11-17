package download

import (
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/cache"
	"github.com/flanksource/deps/pkg/checksum"
	"github.com/flanksource/deps/pkg/utils"
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
				from := utils.ShortenURL(via[len(via)-1].URL.String())
				to := utils.ShortenURL(req.URL.String())
				t.V(4).Infof("Redirect: %s → %s", from, to)
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
	checksumNames    []string // Logical variable names for multiple checksum files
	checksumExpr     string   // CEL expression for extracting checksum from files
	simpleMode       bool     // No task/progress support
	cacheDir         string   // Directory for download cache
	os               string   // Operating system for CEL expressions
	arch             string   // Architecture for CEL expressions
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

// WithChecksumURLsAndNames sets multiple checksum URLs with their logical variable names and optional CEL expression
func WithChecksumURLsAndNames(urls []string, names []string, expr string) DownloadOption {
	return func(c *downloadConfig) {
		c.checksumURLs = urls
		c.checksumNames = names
		c.checksumExpr = strings.TrimSpace(expr)
	}
}

// WithSimpleMode disables task/progress support for simple downloads
func WithSimpleMode() DownloadOption {
	return func(c *downloadConfig) {
		c.simpleMode = true
	}
}

// WithCacheDir sets the cache directory for downloads
func WithCacheDir(cacheDir string) DownloadOption {
	return func(c *downloadConfig) {
		c.cacheDir = strings.TrimSpace(cacheDir)
	}
}

// WithPlatform sets the OS and architecture for CEL expressions in checksum evaluation
func WithPlatform(os, arch string) DownloadOption {
	return func(c *downloadConfig) {
		c.os = os
		c.arch = arch
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
					utils.FormatBytes(pr.current),
					utils.FormatBytes(pr.total),
					speed/1024/1024,
					formatDuration(eta)))
			}
		} else {
			pr.task.SetDescription(fmt.Sprintf("Downloaded %s", utils.FormatBytes(pr.current)))
		}
		pr.lastUpdate = now
	}

	return n, err
}

// fetchChecksumFromURL fetches checksum from a single URL
func fetchChecksumFromURL(checksumURL, downloadURL string, t *task.Task) (checksumValue, checksumType string, sources []string, err error) {
	utils.LogChecksumFetch(t, []string{checksumURL})

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
// Returns: checksumValue, checksumType, overrideURL, sources, error
// If the CEL expression returns a URL, it's included as overrideURL for the caller to use
func fetchChecksumFromMultipleURLs(checksumURLs []string, checksumNames []string, checksumExpr, downloadURL, os, arch string, t *task.Task) (checksumValue, checksumType, overrideURL string, sources []string, err error) {
	checksumContents := make(map[string]string)
	allSources := []string{}

	utils.LogChecksumFetch(t, checksumURLs)

	// Download all checksum files
	for _, checksumURL := range checksumURLs {

		client := createHTTPClient(t)
		resp, err := client.Get(checksumURL)
		if err != nil {
			return "", "", "", nil, fmt.Errorf("failed to download checksum file %s: %w", checksumURL, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", "", "", nil, fmt.Errorf("checksum file not found at %s: status %d", checksumURL, resp.StatusCode)
		}

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", "", "", nil, fmt.Errorf("failed to read checksum file %s: %w", checksumURL, err)
		}

		checksumContents[checksumURL] = string(content)
		allSources = append(allSources, checksumURL)
	}

	// Parse checksum using CEL expression or fallback to simple parsing
	if checksumExpr != "" {
		// Build vars map for CEL evaluation
		vars := make(map[string]interface{})

		// Add filename from download URL
		vars["filename"] = filepath.Base(downloadURL)

		// Add platform info
		if os != "" {
			vars["os"] = os
		}
		if arch != "" {
			vars["arch"] = arch
		}

		// Add checksum file contents with logical names
		for i, url := range checksumURLs {
			content := checksumContents[url]
			// Use logical name if provided, otherwise use index-based fallback
			var varName string
			if checksumNames != nil && i < len(checksumNames) {
				name := checksumNames[i]
				// If the name looks like a URL or has special characters, extract a cleaner name
				if strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://") || strings.Contains(name, "{{") {
					// Use a generic name for URL-based checksum files
					if i == 0 {
						varName = "api"
					} else {
						varName = fmt.Sprintf("api_%d", i)
					}
				} else {
					varName = name
				}
			} else {
				// Fallback to positional names if logical names not provided
				varName = fmt.Sprintf("checksum_%d", i)
			}

			// Try to parse content as JSON, otherwise keep as string
			var jsonData interface{}
			if jsonErr := json.Unmarshal([]byte(content), &jsonData); jsonErr == nil {
				vars[varName] = jsonData
			} else {
				vars[varName] = content
			}
		}

		checksumValue, checksumHashType, discoveredURL, err := checksum.EvaluateCELExpression(vars, checksumExpr)
		if discoveredURL != "" {
			t.V(3).Infof("Evaluated checksum using expression: %s -> checksum=%s, type=%s, url=%s", checksumExpr, checksumValue, checksumHashType, discoveredURL)
		} else {
			t.V(3).Infof("Evaluated checksum using expression: %s -> %s, %s", checksumExpr, checksumValue, checksumHashType)
		}
		if err != nil {
			return "", "", "", nil, err
		}
		return checksumValue, string(checksumHashType), discoveredURL, allSources, nil
	} else {
		// Try to parse from all files, return first successful match
		for _, checksumURL := range checksumURLs {
			content := checksumContents[checksumURL]
			checksumValue, checksumHashType, err := checksum.ParseChecksumFile(content, downloadURL)
			if err == nil && checksumValue != "" {
				return checksumValue, string(checksumHashType), "", allSources, nil
			}
		}
		return "", "", "", nil, fmt.Errorf("no valid checksum found in any of the checksum files")
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

	// Check cache first
	filename := filepath.Base(dest)
	if cachePath, isCached := cache.IsCached(config.cacheDir, url, filename); isCached {
		if t != nil {
			t.V(3).Infof("Found in cache: %s", cachePath)
		}

		// If we have a checksum, validate cached file
		if config.expectedChecksum != "" {
			// Always parse to strip any prefix (e.g., "sha256:") from the checksum value
			var checksumType checksum.HashType
			var err error
			config.expectedChecksum, checksumType, err = checksum.ParseChecksumWithType(config.expectedChecksum)
			if err == nil {
				// Only override checksumType if it wasn't already set
				if config.checksumType == "" {
					config.checksumType = string(checksumType)
				} else {
					checksumType = checksum.HashType(config.checksumType)
				}
			} else {
				// If parsing failed but checksumType is already set, use it
				if config.checksumType != "" {
					checksumType = checksum.HashType(config.checksumType)
				}
			}

			// Validate cached file checksum
			if checksumType != "" {
				hasher, err := checksum.CreateHasher(checksumType)
				if err == nil {
					f, err := os.Open(cachePath)
					if err == nil {
						defer f.Close()
						io.Copy(hasher, f)
						actualChecksum := fmt.Sprintf("%x", hasher.Sum(nil))

						if actualChecksum == config.expectedChecksum {
							if err := cache.CopyFromCache(cachePath, dest); err != nil {
								if t != nil {
									t.V(3).Infof("Failed to copy from cache, will re-download: %v", err)
								}
							} else {
								// Log successful checksum verification for cached file
								if t != nil {
									checksumDisplay := actualChecksum
									if len(checksumDisplay) > 16 {
										checksumDisplay = checksumDisplay[:16] + "..."
									}
									t.Infof("✓ Checksum verified: %s:%s (cached)", config.checksumType, checksumDisplay)
									t.SetDescription(fmt.Sprintf("Copied from cache (%s)", utils.FormatBytes(0)))
								}
								return nil
							}
						} else {
							if t != nil {
								t.V(3).Infof("Cached file checksum mismatch, will re-download")
							}
						}
					}
				}
			}
		} else {
			if err := cache.CopyFromCache(cachePath, dest); err != nil {
				if t != nil {
					t.V(3).Infof("Failed to copy from cache, will re-download: %v", err)
				}
			} else {
				// Log no checksum warning for cached file
				if t != nil {
					msg := api.Text{Content: "✗ No checksum available - copied from cache without validation", Style: "text-red-500"}
					t.Infof(msg.ANSI())
					t.SetDescription(fmt.Sprintf("Copied from cache (%s)", utils.FormatBytes(0)))
				}
				return nil
			}
		}
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

	// Download logging is handled by utils.LogDownloadStart if needed

	// Pre-download checksum and URL discovery
	// If checksum_expr is configured, fetch the checksum (and potentially URL) before downloading
	// This allows API-based asset discovery where both URL and checksum come from the same API response
	actualDownloadURL := url
	var prediscoveredChecksum string

	if len(config.checksumURLs) > 0 && config.checksumExpr != "" {
		// Fetch checksum (and potentially URL) from API before downloading
		checksumValue, checksumType, discoveredURL, checksumSources, err := fetchChecksumFromMultipleURLs(
			config.checksumURLs, config.checksumNames, config.checksumExpr, url, config.os, config.arch, t)
		if err == nil {
			prediscoveredChecksum = checksumValue
			// Format checksum with type prefix for later parsing
			config.expectedChecksum = checksum.FormatChecksum(checksumValue, checksum.HashType(checksumType))
			if config.checksumType == "" {
				config.checksumType = checksumType
			}
			if config.checksumSource == "" {
				config.checksumSource = strings.Join(checksumSources, ",")
			}
			// If a URL was discovered from the API, use it instead of the original URL
			if discoveredURL != "" {
				actualDownloadURL = discoveredURL
				if t != nil {
					t.V(3).Infof("Using discovered URL from API: %s", discoveredURL)
				}
			}
		} else if t != nil {
			t.V(2).Infof("Failed to pre-fetch checksum, will attempt post-download: %v", err)
		}
	}

	// Create HTTP client with redirect logging
	client := createHTTPClient(t)

	// Get the data using the actual download URL (either original or discovered from API)
	resp, err := client.Get(actualDownloadURL)
	if err != nil {
		return fmt.Errorf("failed to download from %s: %w", url, err)
	}
	defer resp.Body.Close()

	// Capture final URL after redirects for checksum verification
	// This ensures we match against the actual filename, not the original URL with query params
	finalURL := resp.Request.URL.String()

	// Try to get filename from Content-Disposition header if available
	// This handles cases where the URL has query parameters (e.g., Azure storage)
	if contentDisp := resp.Header.Get("Content-Disposition"); contentDisp != "" {
		// Parse filename from Content-Disposition header
		// Format: attachment; filename="filename.ext" or attachment; filename=filename.ext
		if idx := strings.Index(contentDisp, "filename="); idx != -1 {
			filenameStr := contentDisp[idx+9:] // Skip "filename="
			filenameStr = strings.TrimSpace(filenameStr)
			filenameStr = strings.Trim(filenameStr, "\"") // Remove quotes if present
			// Use this filename with the base path of finalURL
			if filenameStr != "" {
				// Construct a clean URL with just the path and filename
				finalURL = filepath.Join(filepath.Dir(finalURL), filenameStr)
			}
		}
	} else {
		// No Content-Disposition, strip query parameters from URL
		if idx := strings.Index(finalURL, "?"); idx != -1 {
			finalURL = finalURL[:idx]
		}
	}

	if t != nil && resp.ContentLength > 0 {
		t.SetDescription(fmt.Sprintf("Downloading (%s)", utils.FormatBytes(resp.ContentLength)))
	}

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d %s for %s", resp.StatusCode, resp.Status, url)
	}

	var checksumType checksum.HashType
	if config.expectedChecksum != "" {
		var err error
		// Always parse to strip any prefix (e.g., "sha256:") from the checksum value
		config.expectedChecksum, checksumType, err = checksum.ParseChecksumWithType(config.expectedChecksum)
		if err != nil {
			return fmt.Errorf("invalid checksum format: %w", err)
		}
		// Only override checksumType if it wasn't already set
		if config.checksumType == "" {
			config.checksumType = string(checksumType)
		} else {
			checksumType = checksum.HashType(config.checksumType)
		}
	} else if config.checksumType != "" {
		checksumType = checksum.HashType(config.checksumType)
	}

	var reader io.Reader = resp.Body
	var writer io.Writer = out
	var hasher hash.Hash

	// Only create hasher if we already know the checksum type
	// If we need to fetch checksum from URL, we'll hash the file after download
	needsDeferredHashing := (config.checksumURL != "" || len(config.checksumURLs) > 0) && checksumType == ""

	if checksumType != "" && config.expectedChecksum != "" {
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
	// Skip if we already fetched it pre-download (prediscoveredChecksum is set)
	if config.checksumURL != "" && prediscoveredChecksum == "" {
		checksumValue, checksumType, checksumSources, err := fetchChecksumFromURL(config.checksumURL, finalURL, t)
		if err != nil {
			return fmt.Errorf("failed to fetch checksum: %w", err)
		}
		// Format checksum with type prefix for later parsing
		config.expectedChecksum = checksum.FormatChecksum(checksumValue, checksum.HashType(checksumType))
		if config.checksumType == "" {
			config.checksumType = checksumType
		}
		if config.checksumSource == "" {
			config.checksumSource = strings.Join(checksumSources, ",")
		}
	} else if len(config.checksumURLs) > 0 && prediscoveredChecksum == "" {
		// Only fetch post-download if we didn't already fetch pre-download
		checksumValue, checksumType, discoveredURL, checksumSources, err := fetchChecksumFromMultipleURLs(config.checksumURLs, config.checksumNames, config.checksumExpr, finalURL, config.os, config.arch, t)
		if err != nil {
			return fmt.Errorf("failed to fetch checksum: %w", err)
		}
		// Format checksum with type prefix for later parsing
		config.expectedChecksum = checksum.FormatChecksum(checksumValue, checksum.HashType(checksumType))
		if config.checksumType == "" {
			config.checksumType = checksumType
		}
		if config.checksumSource == "" {
			config.checksumSource = strings.Join(checksumSources, ",")
		}
		// Note: discoveredURL from post-download is ignored since we already downloaded
		if discoveredURL != "" && t != nil {
			t.V(4).Infof("Discovered URL post-download (ignored): %s", discoveredURL)
		}
	}

	// Verify checksum
	if config.expectedChecksum != "" {
		var actualChecksum string

		// If we deferred hashing (because we needed to fetch checksum type from URL),
		// compute the hash now with the correct algorithm
		if needsDeferredHashing {
			// Re-open the file and compute hash with correct type
			f, err := os.Open(tempFile)
			if err != nil {
				return fmt.Errorf("failed to open file for hashing: %w", err)
			}
			defer f.Close()

			hasher, err := checksum.CreateHasher(checksum.HashType(config.checksumType))
			if err != nil {
				return fmt.Errorf("failed to create hasher for type %s: %w", config.checksumType, err)
			}

			if _, err := io.Copy(hasher, f); err != nil {
				return fmt.Errorf("failed to compute checksum: %w", err)
			}

			actualChecksum = fmt.Sprintf("%x", hasher.Sum(nil))
		} else if hasher != nil {
			// Use the hash that was computed during download
			actualChecksum = fmt.Sprintf("%x", hasher.Sum(nil))
		} else {
			// No hasher available - shouldn't happen but handle gracefully
			return fmt.Errorf("cannot verify checksum: no hash computed")
		}

		// Compare checksums
		if !checksum.ChecksumsMatch(config.expectedChecksum, actualChecksum) {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", config.expectedChecksum, actualChecksum)
		}

		// Log successful verification with prominent message
		if t != nil {
			checksumDisplay := actualChecksum
			if len(checksumDisplay) > 16 {
				checksumDisplay = checksumDisplay[:16] + "..."
			}

			displayType := config.checksumType
			if displayType == "" {
				displayType = string(checksumType)
			}

			if config.checksumSource != "" {
				t.Infof("✓ Checksum verified: %s:%s (from %s)",
					displayType, checksumDisplay, utils.ShortenURL(config.checksumSource))
			} else {
				t.Infof("✓ Checksum verified: %s:%s",
					displayType, checksumDisplay)
			}
		}
	} else {
		// No checksum available - log warning at Info level with red color
		if t != nil {
			msg := api.Text{Content: "✗ No checksum available - downloaded without validation", Style: "text-red-500"}
			t.Infof(msg.ANSI())
		}
	}

	// Atomically move temp file to final destination
	if err := os.Rename(tempFile, dest); err != nil {
		return fmt.Errorf("failed to move temp file to destination: %w", err)
	}

	// Save to cache after successful download and verification
	if config.cacheDir != "" {
		if err := cache.SaveToCache(config.cacheDir, url, dest); err != nil {
			// Log but don't fail the download if caching fails
			if t != nil {
				t.V(3).Infof("Failed to save to cache: %v", err)
			}
		} else {
			if t != nil {
				t.V(3).Infof("Saved to cache: %s", filepath.Base(dest))
			}
		}
	}

	if t != nil {
		t.SetDescription(fmt.Sprintf("Downloaded %s (%s)",
			filepath.Base(dest), utils.FormatBytes(written)))
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
