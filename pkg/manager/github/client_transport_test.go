package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDoRESTRequestUsesConfiguredHTTPClient(t *testing.T) {
	called := false
	client := &GitHubClient{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				called = true
				if req.URL.String() != "https://api.github.com/repos/test/tool/tags" {
					t.Fatalf("unexpected URL: %s", req.URL.String())
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[]`)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	var result []map[string]any
	if err := client.doRESTRequest(context.Background(), http.MethodGet, "/repos/test/tool/tags", &result); err != nil {
		t.Fatalf("doRESTRequest failed: %v", err)
	}
	if !called {
		t.Fatalf("expected configured HTTP client to be used")
	}
}
