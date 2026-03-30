package download

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flanksource/clicky/task"
)

func TestSimpleDownloadUsesSingleRequest(t *testing.T) {
	requests := 0
	previousFactory := downloadHTTPClientFactory
	t.Cleanup(func() {
		downloadHTTPClientFactory = previousFactory
	})

	downloadHTTPClientFactory = func(_ *task.Task, _ time.Duration) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewBufferString("payload")),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}),
		}
	}

	dest := filepath.Join(t.TempDir(), "artifact.txt")
	resp, err := SimpleDownload("https://example.com/artifact.txt", dest)
	if err != nil {
		t.Fatalf("SimpleDownload failed: %v", err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 response, got %#v", resp)
	}
	if requests != 1 {
		t.Fatalf("expected a single request, got %d", requests)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(content) != "payload" {
		t.Fatalf("unexpected downloaded content: %q", string(content))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
