package http

import (
	"net/http"
	"time"

	commonshttp "github.com/flanksource/commons/http"
	"github.com/flanksource/commons/logger"
)

// ClientOption configures the HTTP client
type ClientOption func(*clientConfig)

type clientConfig struct {
	timeout      time.Duration
	headerLevel  logger.LogLevel
	bodyLevel    logger.LogLevel
	enableLogger bool
}

// WithTimeout sets the request timeout
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *clientConfig) {
		c.timeout = timeout
	}
}

// WithHttpLogging enables HTTP logging with specified levels
func WithHttpLogging(headerLevel, bodyLevel logger.LogLevel) ClientOption {
	return func(c *clientConfig) {
		c.headerLevel = headerLevel
		c.bodyLevel = bodyLevel
		c.enableLogger = true
	}
}

// GetHttpClient returns a configured HTTP client suitable for general use.
// It uses flanksource/commons/http for consistent logging and middleware support.
// By default, logging is enabled at Debug level for headers and Trace level for bodies.
func GetHttpClient(opts ...ClientOption) *http.Client {
	cfg := &clientConfig{
		timeout:      30 * time.Second,
		headerLevel:  logger.Trace1,
		bodyLevel:    logger.Trace2,
		enableLogger: logger.IsTraceEnabled(),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	client := commonshttp.NewClient().
		Timeout(cfg.timeout)

	if cfg.enableLogger {
		client = client.WithHttpLogging(cfg.headerLevel, cfg.bodyLevel)
	}

	// Convert to standard http.Client by using the RoundTripper
	return &http.Client{
		Transport: client,
		Timeout:   cfg.timeout,
	}
}
