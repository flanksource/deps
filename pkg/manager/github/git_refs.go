package github

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	depshttp "github.com/flanksource/deps/pkg/http"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
)

// GitRef represents a git reference (tag or branch)
type GitRef struct {
	Name string // Full ref name (e.g., "refs/tags/v1.0.0")
	SHA  string // Commit SHA
}

// DiscoverVersionsViaGitOptions configures version discovery behavior
type DiscoverVersionsViaGitOptions struct {
	// SkipSemverFilter skips filtering non-semver tags (useful when version_expr will transform them)
	SkipSemverFilter bool
}

// DiscoverVersionsViaGit fetches tags from a GitHub repository using the git HTTP protocol.
// This avoids GitHub API rate limits by using the git-upload-pack protocol.
// URL format: https://github.com/{owner}/{repo}.git/info/refs?service=git-upload-pack
func DiscoverVersionsViaGit(ctx context.Context, owner, repo string, opts ...DiscoverVersionsViaGitOptions) ([]types.Version, error) {
	var options DiscoverVersionsViaGitOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	url := fmt.Sprintf("https://github.com/%s/%s.git/info/refs?service=git-upload-pack", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// GitHub checks user-agent to determine response format
	req.Header.Set("User-Agent", "git/2.20.1")

	client := depshttp.GetHttpClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch git refs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d from git info/refs", resp.StatusCode)
	}

	refs, err := parseGitUploadPackRefs(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse git refs: %w", err)
	}

	// Filter to only refs/tags/
	type rawTag struct {
		tagName string
		sha     string
	}
	var rawTags []rawTag
	for _, ref := range refs {
		if !strings.HasPrefix(ref.Name, "refs/tags/") {
			continue
		}
		// Skip peeled refs (^{} suffix indicates dereferenced tag)
		if strings.HasSuffix(ref.Name, "^{}") {
			continue
		}
		tagName := strings.TrimPrefix(ref.Name, "refs/tags/")
		rawTags = append(rawTags, rawTag{tagName: tagName, sha: ref.SHA})
	}

	// Count valid semantic versions to detect mixed tag scenarios
	validCount := 0
	for _, t := range rawTags {
		if version.IsValidSemanticVersion(t.tagName) {
			validCount++
		}
	}

	// If we have mixed versions (some valid, some invalid), filter to only valid semver tags
	// Skip this filtering if SkipSemverFilter is set (version_expr will handle transformation)
	filterInvalid := !options.SkipSemverFilter && validCount > 0 && validCount < len(rawTags)
	if filterInvalid {
		logger.V(3).Infof("Git HTTP: Detected %d valid semver tags out of %d total for %s/%s, filtering invalid tags", validCount, len(rawTags), owner, repo)
	}

	var versions []types.Version
	for _, t := range rawTags {
		if filterInvalid && !version.IsValidSemanticVersion(t.tagName) {
			continue
		}

		normalizedVersion := version.Normalize(t.tagName)
		v := types.ParseVersion(normalizedVersion, t.tagName)
		v.SHA = t.sha
		versions = append(versions, v)
	}

	// Sort versions in descending order (newest first)
	version.SortVersions(versions)

	return versions, nil
}

// parseGitUploadPackRefs parses the git-upload-pack protocol response
// Format: PKT-LINE format with 4-byte hex length prefix
// First line contains capabilities separated by NUL byte
func parseGitUploadPackRefs(r io.Reader) ([]GitRef, error) {
	reader := bufio.NewReader(r)
	var refs []GitRef
	firstLine := true

	for {
		// Read 4-byte hex length prefix
		lengthBytes := make([]byte, 4)
		_, err := io.ReadFull(reader, lengthBytes)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read packet length: %w", err)
		}

		length, err := strconv.ParseInt(string(lengthBytes), 16, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid packet length: %s", string(lengthBytes))
		}

		// Length 0 means flush packet (end of section)
		if length == 0 {
			continue
		}

		// Length includes the 4 bytes we already read
		dataLength := int(length) - 4
		if dataLength <= 0 {
			continue
		}

		data := make([]byte, dataLength)
		_, err = io.ReadFull(reader, data)
		if err != nil {
			return nil, fmt.Errorf("failed to read packet data: %w", err)
		}

		line := string(data)

		// First packet is service header (e.g., "# service=git-upload-pack")
		if strings.HasPrefix(line, "# service=") {
			continue
		}

		// First real line contains capabilities after NUL byte
		if firstLine {
			firstLine = false
			// Split on NUL byte to separate ref from capabilities
			parts := strings.SplitN(line, "\x00", 2)
			if len(parts) >= 1 {
				line = parts[0]
			}
		}

		// Remove trailing newline
		line = strings.TrimSuffix(line, "\n")

		// Parse ref line: "<sha> <ref-name>"
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}

		sha := parts[0]
		refName := parts[1]

		// Validate SHA format (40 hex characters)
		if len(sha) != 40 {
			continue
		}

		refs = append(refs, GitRef{
			Name: refName,
			SHA:  sha,
		})
	}

	return refs, nil
}

// DiscoverVersionsViaGitWithFallback tries git HTTP protocol first, falls back to GraphQL
func DiscoverVersionsViaGitWithFallback(ctx context.Context, owner, repo string, limit int, graphqlFallback func() ([]types.Version, error), opts ...DiscoverVersionsViaGitOptions) ([]types.Version, error) {
	// Try git HTTP protocol first (no rate limits)
	versions, err := DiscoverVersionsViaGit(ctx, owner, repo, opts...)
	if err == nil {
		return versions, nil
	}

	logger.V(2).Infof("Git HTTP failed for %s/%s: %v, falling back to GraphQL", owner, repo, err)

	// Fall back to GraphQL if git HTTP fails
	if graphqlFallback != nil {
		return graphqlFallback()
	}

	return nil, err
}

// FindTagByVersionViaGit searches for a specific version tag using git HTTP protocol
func FindTagByVersionViaGit(ctx context.Context, owner, repo, targetVersion, versionExpr string) (string, string, error) {
	versions, err := DiscoverVersionsViaGit(ctx, owner, repo)
	if err != nil {
		return "", "", err
	}

	// Try exact tag match first
	for _, ver := range versions {
		if ver.Tag == targetVersion || ver.Tag == "v"+targetVersion {
			return ver.Tag, ver.SHA, nil
		}
	}

	// Try version normalization match
	normalizedTarget := version.Normalize(targetVersion)
	for _, ver := range versions {
		if version.Normalize(ver.Tag) == normalizedTarget {
			return ver.Tag, ver.SHA, nil
		}
	}

	// If version_expr is provided, try applying it to each tag
	if versionExpr != "" {
		for _, ver := range versions {
			testVersion := types.Version{
				Tag:     ver.Tag,
				Version: version.Normalize(ver.Tag),
			}
			transformed, err := version.ApplyVersionExpr([]types.Version{testVersion}, versionExpr)
			if err != nil {
				continue
			}

			if len(transformed) > 0 && transformed[0].Version == targetVersion {
				return ver.Tag, ver.SHA, nil
			}
		}
	}

	return "", "", fmt.Errorf("version %s not found in %s/%s", targetVersion, owner, repo)
}

// gitRefsCacheEntry represents a cached git refs response
type gitRefsCacheEntry struct {
	versions  []types.Version
	fetchedAt time.Time
}

// gitRefsCache caches git refs responses to avoid repeated HTTP calls
var gitRefsCache = make(map[string]*gitRefsCacheEntry)
var gitRefsCacheTTL = 5 * time.Minute

// DiscoverVersionsViaGitCached is like DiscoverVersionsViaGit but with caching
func DiscoverVersionsViaGitCached(ctx context.Context, owner, repo string, limit int) ([]types.Version, error) {
	cacheKey := fmt.Sprintf("%s/%s", owner, repo)

	// Check cache
	if entry, ok := gitRefsCache[cacheKey]; ok {
		if time.Since(entry.fetchedAt) < gitRefsCacheTTL {
			versions := entry.versions
			if limit > 0 && len(versions) > limit {
				versions = versions[:limit]
			}
			return versions, nil
		}
	}

	// Fetch fresh data
	versions, err := DiscoverVersionsViaGit(ctx, owner, repo) // Get all, cache full list
	if err != nil {
		return nil, err
	}

	// Update cache
	gitRefsCache[cacheKey] = &gitRefsCacheEntry{
		versions:  versions,
		fetchedAt: time.Now(),
	}

	// Apply limit
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}

	return versions, nil
}
