package http

import (
	"testing"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/commons/properties"
)

func TestResolveHTTPLogConfigDefaultsToAccess(t *testing.T) {
	cfg := &clientConfig{}

	traceConfig, ok := resolveHTTPLogConfig(cfg)
	if !ok {
		t.Fatalf("expected default HTTP logging to be enabled")
	}
	if !traceConfig.AccessLog {
		t.Fatalf("expected default HTTP logging mode to be access")
	}
}

func TestResolveHTTPLogConfigSupportsLegacyPluralProperty(t *testing.T) {
	restoreHTTPLogProperties(t)
	properties.Set("http.logs", "body")

	cfg := &clientConfig{
		logMode: properties.String("access", "http.log", "http.logs"),
	}

	traceConfig, ok := resolveHTTPLogConfig(cfg)
	if !ok {
		t.Fatalf("expected HTTP logging to be enabled")
	}
	if !traceConfig.Body || !traceConfig.Response {
		t.Fatalf("expected body logging when http.logs=body")
	}
}

func TestResolveHTTPLogConfigPrefersSingularProperty(t *testing.T) {
	restoreHTTPLogProperties(t)
	properties.Set("http.logs", "access")
	properties.Set("http.log", "headers")

	cfg := &clientConfig{
		logMode: properties.String("access", "http.log", "http.logs"),
	}

	traceConfig, ok := resolveHTTPLogConfig(cfg)
	if !ok {
		t.Fatalf("expected HTTP logging to be enabled")
	}
	if traceConfig.AccessLog {
		t.Fatalf("expected singular http.log property to take precedence")
	}
	if !traceConfig.Headers || !traceConfig.ResponseHeaders {
		t.Fatalf("expected headers logging when http.log=headers")
	}
}

func TestResolveHTTPLogConfigCanDisableLogging(t *testing.T) {
	cfg := &clientConfig{logMode: "off"}

	_, ok := resolveHTTPLogConfig(cfg)
	if ok {
		t.Fatalf("expected HTTP logging to be disabled")
	}
}

func TestResolveThresholdHTTPLogConfig(t *testing.T) {
	currentLevel := logger.GetLogger().GetLevel()
	t.Cleanup(func() {
		logger.GetLogger().SetLogLevel(currentLevel)
	})

	logger.GetLogger().SetLogLevel(logger.Trace1)
	traceConfig, ok := resolveThresholdHTTPLogConfig(logger.Trace1, logger.Trace2)
	if !ok {
		t.Fatalf("expected threshold-based logging to be enabled at Trace1")
	}
	if !traceConfig.Headers || traceConfig.Body {
		t.Fatalf("expected header-only logging at Trace1")
	}

	logger.GetLogger().SetLogLevel(logger.Trace2)
	traceConfig, ok = resolveThresholdHTTPLogConfig(logger.Trace1, logger.Trace2)
	if !ok {
		t.Fatalf("expected threshold-based logging to be enabled at Trace2")
	}
	if !traceConfig.Body || !traceConfig.Response {
		t.Fatalf("expected body logging at Trace2")
	}
}

func restoreHTTPLogProperties(t *testing.T) {
	t.Helper()

	originalLog := properties.Get("http.log")
	originalLogs := properties.Get("http.logs")
	t.Cleanup(func() {
		properties.Set("http.log", originalLog)
		properties.Set("http.logs", originalLogs)
	})

	properties.Set("http.log", "")
	properties.Set("http.logs", "")
}
