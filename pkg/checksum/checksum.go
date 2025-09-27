package checksum

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/gomplate/v3"
	"gopkg.in/yaml.v3"
)

// HashType represents different hash algorithms
type HashType string

const (
	HashTypeMD5    HashType = "md5"
	HashTypeSHA1   HashType = "sha1"
	HashTypeSHA256 HashType = "sha256"
	HashTypeSHA384 HashType = "sha384"
	HashTypeSHA512 HashType = "sha512"
)

// DetectHashType detects the hash type from a checksum string
func DetectHashType(checksum string) HashType {
	checksum = strings.TrimSpace(checksum)

	// Check for explicit prefix
	if strings.Contains(checksum, ":") {
		parts := strings.SplitN(checksum, ":", 2)
		if len(parts) == 2 {
			prefix := strings.ToLower(strings.TrimSpace(parts[0]))
			switch prefix {
			case "md5":
				return HashTypeMD5
			case "sha1":
				return HashTypeSHA1
			case "sha256":
				return HashTypeSHA256
			case "sha384":
				return HashTypeSHA384
			case "sha512":
				return HashTypeSHA512
			}
		}
	}

	// Fall back to detection by length (assuming hex encoding)
	// Remove any prefix and whitespace
	if idx := strings.Index(checksum, ":"); idx >= 0 {
		checksum = checksum[idx+1:]
	}
	checksum = strings.TrimSpace(checksum)

	switch len(checksum) {
	case 32:
		return HashTypeMD5
	case 40:
		return HashTypeSHA1
	case 64:
		return HashTypeSHA256
	case 96:
		return HashTypeSHA384
	case 128:
		return HashTypeSHA512
	default:
		return HashTypeSHA256 // Default fallback
	}
}

// CreateHasher creates the appropriate hash.Hash for the given type
func CreateHasher(hashType HashType) (hash.Hash, error) {
	switch hashType {
	case HashTypeMD5:
		return md5.New(), nil
	case HashTypeSHA1:
		return sha1.New(), nil
	case HashTypeSHA256:
		return sha256.New(), nil
	case HashTypeSHA384:
		return sha512.New384(), nil
	case HashTypeSHA512:
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("unsupported hash type: %s", hashType)
	}
}

// ParseChecksum extracts the checksum value and type from a string
// This function is lenient and will try to guess the type if not specified
func ParseChecksum(checksum string) (value string, hashType HashType) {
	checksum = strings.TrimSpace(checksum)

	if strings.Contains(checksum, ":") {
		parts := strings.SplitN(checksum, ":", 2)
		if len(parts) == 2 {
			hashType = DetectHashType(parts[0])
			value = strings.TrimSpace(parts[1])
			return
		}
	}

	// No prefix, detect by length
	hashType = DetectHashType(checksum)
	value = checksum
	return
}

// ParseChecksumWithType parses a checksum that requires a type prefix (e.g., "sha256:abc123")
// Returns an error if no type prefix is found
func ParseChecksumWithType(checksum string) (value string, hashType HashType, err error) {
	checksum = strings.TrimSpace(checksum)

	// Check for type prefix
	if strings.Contains(checksum, ":") {
		parts := strings.SplitN(checksum, ":", 2)
		if len(parts) == 2 {
			typeStr := strings.ToLower(strings.TrimSpace(parts[0]))
			value = strings.TrimSpace(parts[1])
			hashType = HashType(typeStr)
			return
		}
	}

	// No prefix, return error requiring explicit type
	return "", "", fmt.Errorf("checksum type not specified (expected format 'type:checksum'), got: %s", checksum)
}

// FormatChecksum formats a checksum with its type prefix
func FormatChecksum(value string, hashType HashType) string {
	return fmt.Sprintf("%s:%s", hashType, value)
}

// CalculateBinaryChecksum calculates the checksum of a binary file
func CalculateBinaryChecksum(filePath string, hashType HashType) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hasher, err := CreateHasher(hashType)
	if err != nil {
		return "", err
	}

	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// Discovery handles checksum discovery using multiple strategies
type Discovery struct {
	strategies []Strategy
}

// Strategy defines an interface for different checksum discovery methods
type Strategy interface {
	Name() string
	FindChecksums(ctx context.Context, resolution *types.Resolution) (map[string]string, error)
}

// NewDiscovery creates a new checksum discovery service with default strategies
func NewDiscovery() *Discovery {
	return &Discovery{
		strategies: []Strategy{
			&GoreleaserStrategy{},
			&HashiCorpStrategy{},
			&IndividualFileStrategy{},
			&GitHubReleaseBodyStrategy{},
		},
	}
}

// AddStrategy adds a custom checksum strategy
func (d *Discovery) AddStrategy(strategy Strategy) {
	d.strategies = append(d.strategies, strategy)
}

// FindChecksums tries all strategies to find checksums for a resolution
func (d *Discovery) FindChecksums(ctx context.Context, resolution *types.Resolution) (map[string]string, error) {
	var lastErr error

	for _, strategy := range d.strategies {
		checksums, err := strategy.FindChecksums(ctx, resolution)
		if err != nil {
			lastErr = err
			continue
		}

		if len(checksums) > 0 {
			return checksums, nil
		}
	}

	return nil, fmt.Errorf("no checksums found using any strategy: %v", lastErr)
}

// CalculateFileChecksum downloads a file and calculates its SHA256 checksum
func CalculateFileChecksum(ctx context.Context, url string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("failed to download file from %s: status %d", url, resp.StatusCode)
	}

	hasher := sha256.New()
	size, err := io.Copy(hasher, resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read file: %w", err)
	}

	checksum := fmt.Sprintf("sha256:%x", hasher.Sum(nil))
	return checksum, size, nil
}

// GoreleaserStrategy handles checksums.txt files generated by goreleaser
type GoreleaserStrategy struct{}

func (g *GoreleaserStrategy) Name() string {
	return "goreleaser"
}

func (g *GoreleaserStrategy) FindChecksums(ctx context.Context, resolution *types.Resolution) (map[string]string, error) {
	if resolution.ChecksumURL != "" {
		return g.parseChecksumFile(ctx, resolution.ChecksumURL)
	}

	// Try to find checksums.txt in the same release
	if resolution.GitHubAsset != nil {
		// Replace asset name with checksums.txt
		baseURL := strings.TrimSuffix(resolution.DownloadURL, resolution.GitHubAsset.AssetName)
		checksumURL := baseURL + "checksums.txt"

		checksums, err := g.parseChecksumFile(ctx, checksumURL)
		if err == nil && len(checksums) > 0 {
			return checksums, nil
		}
	}

	return nil, fmt.Errorf("no goreleaser checksum file found")
}

func (g *GoreleaserStrategy) parseChecksumFile(ctx context.Context, url string) (map[string]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download checksum file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksum file not found at %s: status %d", url, resp.StatusCode)
	}

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)

	// Goreleaser format: "checksum  filename"
	re := regexp.MustCompile(`^([a-fA-F0-9]+)\s+(.+)$`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if len(matches) == 3 {
			checksum := matches[1]
			filename := matches[2]
			checksums[filename] = "sha256:" + checksum
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading checksum file: %w", err)
	}

	return checksums, nil
}

// HashiCorpStrategy handles SHA256SUMS files from HashiCorp releases
type HashiCorpStrategy struct{}

func (h *HashiCorpStrategy) Name() string {
	return "hashicorp"
}

func (h *HashiCorpStrategy) FindChecksums(ctx context.Context, resolution *types.Resolution) (map[string]string, error) {
	if resolution.ChecksumURL != "" {
		return h.parseChecksumFile(ctx, resolution.ChecksumURL)
	}

	// Try to find SHA256SUMS file
	if resolution.GitHubAsset != nil {
		// Replace asset name with pattern like terraform_1.5.7_SHA256SUMS
		baseURL := strings.TrimSuffix(resolution.DownloadURL, resolution.GitHubAsset.AssetName)

		// Extract product name from asset name (e.g., terraform from terraform_1.5.7_linux_amd64.zip)
		parts := strings.Split(resolution.GitHubAsset.AssetName, "_")
		if len(parts) >= 2 {
			product := parts[0]
			version := resolution.Version
			checksumFile := fmt.Sprintf("%s_%s_SHA256SUMS", product, version)
			checksumURL := baseURL + checksumFile

			checksums, err := h.parseChecksumFile(ctx, checksumURL)
			if err == nil && len(checksums) > 0 {
				return checksums, nil
			}
		}
	}

	return nil, fmt.Errorf("no HashiCorp checksum file found")
}

func (h *HashiCorpStrategy) parseChecksumFile(ctx context.Context, url string) (map[string]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download checksum file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksum file not found at %s: status %d", url, resp.StatusCode)
	}

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)

	// HashiCorp format: "checksum  filename"
	re := regexp.MustCompile(`^([a-fA-F0-9]+)\s+(.+)$`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if len(matches) == 3 {
			checksum := matches[1]
			filename := matches[2]
			checksums[filename] = "sha256:" + checksum
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading checksum file: %w", err)
	}

	return checksums, nil
}

// IndividualFileStrategy looks for individual .sha256 files
type IndividualFileStrategy struct{}

func (i *IndividualFileStrategy) Name() string {
	return "individual_file"
}

func (i *IndividualFileStrategy) FindChecksums(ctx context.Context, resolution *types.Resolution) (map[string]string, error) {
	// Try filename.sha256
	checksumURL := resolution.DownloadURL + ".sha256"

	resp, err := http.Get(checksumURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download checksum file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("individual checksum file not found")
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read checksum file: %w", err)
	}

	checksum := strings.TrimSpace(string(content))
	if !strings.HasPrefix(checksum, "sha256:") {
		checksum = "sha256:" + checksum
	}

	// Get filename from URL
	parts := strings.Split(resolution.DownloadURL, "/")
	filename := parts[len(parts)-1]

	return map[string]string{
		filename: checksum,
	}, nil
}

// GitHubReleaseBodyStrategy extracts checksums from GitHub release descriptions
type GitHubReleaseBodyStrategy struct{}

func (g *GitHubReleaseBodyStrategy) Name() string {
	return "github_release_body"
}

func (g *GitHubReleaseBodyStrategy) FindChecksums(ctx context.Context, resolution *types.Resolution) (map[string]string, error) {
	// This would require GitHub API access to get the release body
	// For now, return not implemented
	return nil, fmt.Errorf("GitHub release body parsing not implemented yet")
}

// VerifyChecksum verifies a file against a checksum
func VerifyChecksum(filePath, expectedChecksum string) error {
	expectedValue, hashType := ParseChecksum(expectedChecksum)

	actualValue, err := CalculateBinaryChecksum(filePath, hashType)
	if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	if strings.ToLower(actualValue) != strings.ToLower(expectedValue) {
		return fmt.Errorf("checksum mismatch for file %s: expected %s:%s, got %s:%s",
			filepath.Base(filePath), hashType, expectedValue, hashType, actualValue)
	}

	return nil
}

// EvaluateCELExpression uses a CEL expression to extract a checksum from checksum file contents
func EvaluateCELExpression(checksumContents map[string]string, fileURL, expr string) (value string, hashType HashType, err error) {
	filename := filepath.Base(fileURL)

	// Prepare variables for evaluation
	vars := map[string]interface{}{
		"filename": filename,
	}
	for name, content := range checksumContents {
		vars[name] = content
	}

	checksumValue, evalErr := gomplate.RunTemplate(vars, gomplate.Template{Expression: expr})
	if evalErr != nil {
		return "", "", evalErr
	}

	if checksumValue == "" {
		// Get list of available checksum file names for debugging
		var fileNames []string
		for name := range checksumContents {
			fileNames = append(fileNames, name)
		}
		return "", "", fmt.Errorf("CEL expression returned empty checksum - expression: %s, filename: %s, checksum files available: %v",
			expr, filename, fileNames)
	}

	// Parse the checksum type and value from CEL result
	// CEL expressions should return format "type:checksum"
	value, hashType, err = ParseChecksumWithType(checksumValue)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse '%s' from CEL result: %w", checksumValue, err)
	}

	return value, hashType, nil
}

// EnvtestReleasesYAML represents the structure of the envtest-releases.yaml file
type EnvtestReleasesYAML struct {
	Releases map[string]map[string]struct {
		Hash     string `yaml:"hash"`
		SelfLink string `yaml:"selfLink"`
	} `yaml:"releases"`
}

// parseEnvtestReleasesYAML attempts to parse content as envtest-releases.yaml format
func parseEnvtestReleasesYAML(content, filename string) (value string, hashType HashType, err error) {
	var releases EnvtestReleasesYAML
	if err := yaml.Unmarshal([]byte(content), &releases); err != nil {
		return "", "", fmt.Errorf("failed to parse as envtest-releases YAML: %w", err)
	}

	// Look for the filename in any version
	for version, files := range releases.Releases {
		if fileData, exists := files[filename]; exists {
			if fileData.Hash == "" {
				return "", "", fmt.Errorf("empty hash for file %s in version %s", filename, version)
			}

			// The hash in envtest-releases.yaml is typically SHA-512
			// It's a plain hex string without prefix
			hashValue := strings.TrimSpace(fileData.Hash)

			// Detect hash type by length
			switch len(hashValue) {
			case 128:
				return hashValue, HashTypeSHA512, nil
			case 64:
				return hashValue, HashTypeSHA256, nil
			case 40:
				return hashValue, HashTypeSHA1, nil
			case 32:
				return hashValue, HashTypeMD5, nil
			default:
				// Default to SHA-512 for envtest releases
				return hashValue, HashTypeSHA512, nil
			}
		}
	}

	return "", "", fmt.Errorf("file %s not found in envtest-releases.yaml", filename)
}

// isValidChecksumFormat checks if a string looks like a valid checksum format
// Supports both plain hex strings and type-prefixed format (e.g., "sha256:abc123")
func isValidChecksumFormat(input string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}

	// Handle type-prefixed format
	if strings.Contains(input, ":") {
		parts := strings.SplitN(input, ":", 2)
		if len(parts) != 2 {
			return false
		}
		// Check if the prefix looks like a hash type
		prefix := strings.ToLower(strings.TrimSpace(parts[0]))
		validPrefixes := []string{"md5", "sha1", "sha256", "sha384", "sha512"}
		validPrefix := false
		for _, p := range validPrefixes {
			if prefix == p {
				validPrefix = true
				break
			}
		}
		if !validPrefix {
			return false
		}
		input = strings.TrimSpace(parts[1])
	}

	// Check if remaining part is hex and has valid length
	if !isHexString(input) {
		return false
	}

	// Valid checksum lengths
	validLengths := []int{32, 40, 64, 96, 128} // MD5, SHA1, SHA256, SHA384, SHA512
	for _, length := range validLengths {
		if len(input) == length {
			return true
		}
	}

	return false
}

// isHexString checks if a string contains only hexadecimal characters
func isHexString(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

// ParseChecksumFile parses a checksum file and extracts the checksum for the given file URL
func ParseChecksumFile(content, fileURL string) (value string, hashType HashType, err error) {
	filename := filepath.Base(fileURL)

	// Try to detect if this is YAML content (envtest-releases.yaml format)
	if strings.Contains(content, "releases:") && strings.Contains(content, "hash:") && strings.Contains(content, "selfLink:") {
		value, hashType, err = parseEnvtestReleasesYAML(content, filename)
		if err == nil {
			return value, hashType, nil
		}
		// If YAML parsing fails, continue with standard parsing as fallback
	}

	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			// Try standard format: "checksum  filename" or "checksum *filename"
			checksumPart := parts[0]
			filePart := strings.Join(parts[1:], " ")

			// Remove leading * if present (binary mode indicator)
			filePart = strings.TrimPrefix(filePart, "*")

			// Check if this line is for our file
			if filePart == filename || strings.HasSuffix(filePart, "/"+filename) {
				// Try to parse checksum with potential type prefix
				if strings.Contains(checksumPart, ":") {
					value, hashType, err = ParseChecksumWithType(checksumPart)
					if err == nil {
						return value, hashType, nil
					}
				}
				// Fall back to detection by length
				value, hashType = ParseChecksum(checksumPart)
				return value, hashType, nil
			}

			// Try yq format: "filename  checksum1  checksum2  ..."
			if parts[0] == filename || strings.HasSuffix(parts[0], "/"+filename) {
				// Find the best checksum from multiple checksums
				// Prefer SHA256 (64 chars), then SHA1 (40 chars), then MD5 (32 chars)
				var bestChecksum string
				var bestType HashType

				for i := 1; i < len(parts); i++ {
					checksum := parts[i]
					var checksumType HashType

					// Determine checksum type by length
					switch len(checksum) {
					case 32:
						checksumType = HashTypeMD5
					case 40:
						checksumType = HashTypeSHA1
					case 64:
						checksumType = HashTypeSHA256
					case 128:
						checksumType = HashTypeSHA512
					default:
						continue // Skip unknown checksum format
					}

					// Prefer SHA256, then SHA1, then others
					if bestType == "" || checksumType == HashTypeSHA256 ||
						(bestType != HashTypeSHA256 && checksumType == HashTypeSHA1) {
						bestChecksum = checksum
						bestType = checksumType
					}
				}

				if bestChecksum != "" {
					return bestChecksum, bestType, nil
				}
			}
		}
	}

	// Try single-line checksum format (entire file contains only the checksum)
	// This is common for files that contain just a checksum value with no filename
	nonEmptyLines := make([]string, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			nonEmptyLines = append(nonEmptyLines, line)
		}
	}

	// If there's exactly one non-empty, non-comment line, treat it as the checksum
	if len(nonEmptyLines) == 1 {
		checksumLine := nonEmptyLines[0]

		// Verify it looks like a checksum (hex chars and optional type prefix)
		if isValidChecksumFormat(checksumLine) {
			// Parse the checksum value and detect type
			value, hashType = ParseChecksum(checksumLine)
			return value, hashType, nil
		}
	}

	return "", "", fmt.Errorf("checksum not found for file %s in checksum file", filename)
}

// DownloadChecksumFiles downloads multiple checksum files and returns their contents
func DownloadChecksumFiles(ctx context.Context, checksumURLs []string) (map[string]string, error) {
	contents := make(map[string]string)

	for _, url := range checksumURLs {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request for %s: %w", url, err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to download checksum file %s: %w", url, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to download checksum file from %s: status %d", url, resp.StatusCode)
		}

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read checksum file %s: %w", url, err)
		}

		// Use the filename as the key
		filename := filepath.Base(url)
		contents[filename] = string(content)
	}

	return contents, nil
}
