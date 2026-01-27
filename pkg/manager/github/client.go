package github

import (
	"context"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v57/github"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// GitHubClient is a singleton wrapper for GitHub API clients
type GitHubClient struct {
	client      *github.Client
	graphql     *githubv4.Client
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
	var graphqlClient *githubv4.Client
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
		tc := oauth2.NewClient(context.Background(), ts)
		client = github.NewClient(tc)
		graphqlClient = githubv4.NewClient(tc)
	} else {
		client = github.NewClient(nil)
		graphqlClient = githubv4.NewClient(nil)
	}

	return &GitHubClient{
		client:      client,
		graphql:     graphqlClient,
		tokenSource: tokenSource,
	}
}

// SetToken updates the GitHub clients with a new token
func (c *GitHubClient) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		tc := oauth2.NewClient(context.Background(), ts)
		c.client = github.NewClient(tc)
		c.graphql = githubv4.NewClient(tc)
		c.tokenSource = "CLI-provided"
	}
}

// Client returns the REST API client
func (c *GitHubClient) Client() *github.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// GraphQL returns the GraphQL client
func (c *GitHubClient) GraphQL() *githubv4.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.graphql
}

// TokenSource returns the current token source name
func (c *GitHubClient) TokenSource() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tokenSource
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

// Query executes a GraphQL query with retry logic for transient errors
func (c *GitHubClient) Query(ctx context.Context, q interface{}, variables map[string]interface{}) error {
	maxRetries := 3
	baseDelay := 500 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := c.GraphQL().Query(ctx, q, variables)
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
