package http

import (
	"net/http"
	"strings"
	"time"

	httpmiddlewares "github.com/flanksource/commons/http/middlewares"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
)

// ClientOption configures the HTTP client
type ClientOption func(*clientConfig)

type clientConfig struct {
	timeout           time.Duration
	headerLevel       logger.LogLevel
	bodyLevel         logger.LogLevel
	logMode           string
	useLevelThreshold bool
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
		c.useLevelThreshold = true
	}
}

// GetHttpClient returns a configured HTTP client suitable for general use.
// It uses the shared commons HTTP logger middleware for consistent HTTP logging.
// We intentionally avoid using commons/http.Client directly as a stdlib Transport
// because its request adaptation re-serializes existing query parameters.
func GetHttpClient(opts ...ClientOption) *http.Client {
	cfg := &clientConfig{
		timeout:     30 * time.Second,
		headerLevel: logger.Trace1,
		bodyLevel:   logger.Trace2,
		logMode:     properties.String("access", "http.log", "http.logs"),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	var transport http.RoundTripper = baseTransport

	if traceConfig, ok := resolveHTTPLogConfig(cfg); ok {
		transport = httpmiddlewares.NewLogger(traceConfig)(transport)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   cfg.timeout,
	}
}

func resolveHTTPLogConfig(cfg *clientConfig) (httpmiddlewares.TraceConfig, bool) {
	if cfg.useLevelThreshold {
		return resolveThresholdHTTPLogConfig(cfg.headerLevel, cfg.bodyLevel)
	}

	mode := strings.TrimSpace(strings.ToLower(cfg.logMode))
	if mode == "" {
		mode = "access"
	}
	switch mode {
	case "off", "false", "disabled", "none":
		return httpmiddlewares.TraceConfig{}, false
	default:
		return httpmiddlewares.TraceConfigFromString(mode), true
	}
}

func resolveThresholdHTTPLogConfig(headerLevel, bodyLevel logger.LogLevel) (httpmiddlewares.TraceConfig, bool) {
	switch {
	case logger.IsLevelEnabled(bodyLevel):
		return httpmiddlewares.TraceConfigFromString("body"), true
	case logger.IsLevelEnabled(headerLevel):
		return httpmiddlewares.TraceConfigFromString("headers"), true
	default:
		return httpmiddlewares.TraceConfig{}, false
	}
}
