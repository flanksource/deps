package github

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	depshttp "github.com/flanksource/deps/pkg/http"
	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

// GitHubClient is a singleton wrapper for GitHub API clients
type GitHubClient struct {
	client      *github.Client
	httpClient  *http.Client
	token       string
	tokenSource string
	mu          sync.RWMutex
}

var (
	clientInstance *GitHubClient
	clientOnce     sync.Once
)

// GetClient returns the singleton GitHubClient instance
func GetClient() *GitHubClient {
	clientOnce.Do(func() {
		clientInstance = newClient("${GITHUB_TOKEN}", "${GH_TOKEN}", "${GITHUB_ACCESS_TOKEN}")
	})
	return clientInstance
}

// newClient creates a GitHub client with token resolution
func newClient(tokenSources ...string) *GitHubClient {
	var client *github.Client
	var httpClient *http.Client
	var token string
	var tokenSource string

	// Try each token source and use first non-empty
	for _, pattern := range tokenSources {
		expanded := os.ExpandEnv(pattern)
		if expanded != "" && expanded != pattern {
			token = expanded
			tokenSource = strings.TrimSuffix(strings.TrimPrefix(pattern, "${"), "}")
			break
		}
	}

	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		httpClient = oauth2.NewClient(context.Background(), ts)
		client = github.NewClient(httpClient)
	} else {
		client = github.NewClient(nil)
		httpClient = depshttp.GetHttpClient()
	}

	return &GitHubClient{
		client:      client,
		httpClient:  httpClient,
		token:       token,
		tokenSource: tokenSource,
	}
}

// SetToken updates the GitHub clients with a new token
func (c *GitHubClient) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		httpClient := oauth2.NewClient(context.Background(), ts)
		c.client = github.NewClient(httpClient)
		c.httpClient = httpClient
		c.token = token
		c.tokenSource = "CLI-provided"
	}
}

// Client returns the REST API client
func (c *GitHubClient) Client() *github.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// TokenSource returns the current token source name
func (c *GitHubClient) TokenSource() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tokenSource
}

// Token returns the current token value
func (c *GitHubClient) Token() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// isRetryableError checks if an error is a retryable HTTP error
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "504 Gateway Timeout") ||
		strings.Contains(errStr, "502 Bad Gateway") ||
		strings.Contains(errStr, "503 Service Unavailable")
}

// RESTRequest makes a REST API request to GitHub with retry logic
func (c *GitHubClient) RESTRequest(ctx context.Context, method, endpoint string, result interface{}) error {
	return c.RESTRequestWithRetry(ctx, method, endpoint, result, 3)
}

// RESTRequestWithRetry makes a REST API request with configurable retries
func (c *GitHubClient) RESTRequestWithRetry(ctx context.Context, method, endpoint string, result interface{}, maxRetries int) error {
	baseDelay := 500 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := c.doRESTRequest(ctx, method, endpoint, result)
		if err == nil {
			return nil
		}

		if !isRetryableError(err) {
			return err
		}

		lastErr = err

		// Don't sleep after the last attempt
		if attempt < maxRetries-1 {
			delay := baseDelay * time.Duration(1<<attempt)
			jitter := time.Duration(rand.Int63n(int64(delay / 2)))
			time.Sleep(delay + jitter)
		}
	}

	return lastErr
}

// doRESTRequest performs a single REST API request
func (c *GitHubClient) doRESTRequest(ctx context.Context, method, endpoint string, result interface{}) error {
	url := "https://api.github.com" + endpoint

	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := depshttp.GetHttpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 404 {
		return fmt.Errorf("not found: %s", endpoint)
	}
	if resp.StatusCode == 403 {
		return fmt.Errorf("rate limit exceeded or forbidden: %s", endpoint)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, endpoint)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
