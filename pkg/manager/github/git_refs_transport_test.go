package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/flanksource/deps/pkg/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDiscoverVersionsViaGitUsesSingleServiceQueryParam(t *testing.T) {
	previousClient := gitRefsHTTPClient
	defer func() { gitRefsHTTPClient = previousClient }()

	gitRefsHTTPClient = func() *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				values := req.URL.Query()["service"]
				if len(values) != 1 || values[0] != "git-upload-pack" {
					t.Fatalf("expected a single service=git-upload-pack query param, got %v", values)
				}

				body := strings.NewReader(
					pktLine("# service=git-upload-pack\n") +
						"0000" +
						pktLine(strings.Repeat("1", 40)+" refs/tags/v1.2.3\x00multi_ack thin-pack\n") +
						"0000",
				)

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(body),
					Header:     make(http.Header),
				}, nil
			}),
		}
	}

	versions, err := DiscoverVersionsViaGit(context.Background(), "golang", "go")
	if err != nil {
		t.Fatalf("DiscoverVersionsViaGit failed: %v", err)
	}
	if len(versions) != 1 || versions[0].Tag != "v1.2.3" {
		t.Fatalf("expected one v1.2.3 tag, got %#v", versions)
	}
}

func TestDiscoverVersionsViaGitWithFallbackOnTransportFailure(t *testing.T) {
	previousClient := gitRefsHTTPClient
	defer func() { gitRefsHTTPClient = previousClient }()

	gitRefsHTTPClient = func() *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
				}, nil
			}),
		}
	}

	fallbackCalled := false
	versions, err := DiscoverVersionsViaGitWithFallback(
		context.Background(),
		"golang",
		"go",
		10,
		func() ([]types.Version, error) {
			fallbackCalled = true
			return []types.Version{{Version: "1.24.0", Tag: "go1.24.0"}}, nil
		},
	)
	if err != nil {
		t.Fatalf("DiscoverVersionsViaGitWithFallback failed: %v", err)
	}
	if !fallbackCalled {
		t.Fatalf("expected fallback to be called")
	}
	if len(versions) != 1 || versions[0].Tag != "go1.24.0" {
		t.Fatalf("unexpected fallback versions: %#v", versions)
	}
}

func pktLine(s string) string {
	return strings.ToLower(hexLen(len(s)+4)) + s
}

func hexLen(n int) string {
	const digits = "0123456789abcdef"
	buf := []byte{'0', '0', '0', '0'}
	for i := 3; i >= 0; i-- {
		buf[i] = digits[n&0xf]
		n >>= 4
	}
	return string(buf)
}
