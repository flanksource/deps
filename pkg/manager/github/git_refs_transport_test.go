package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// redirectClient returns a client func whose transport answers every request with a
// 302 redirect to the given Location (mimicking github.com/.../releases/latest).
func redirectClient(location string) func() *http.Client {
	return func() *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				header := make(http.Header)
				header.Set("Location", location)
				return &http.Response{
					StatusCode: http.StatusFound,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     header,
				}, nil
			}),
		}
	}
}

func TestResolveLatestTagViaRedirect(t *testing.T) {
	previousClient := gitRefsHTTPClient
	defer func() { gitRefsHTTPClient = previousClient }()
	gitRefsHTTPClient = redirectClient("https://github.com/flanksource/mission-control-plugins/releases/tag/v1.7.1")

	tag, err := ResolveLatestTagViaRedirect(context.Background(), "flanksource", "mission-control-plugins")
	if err != nil {
		t.Fatalf("ResolveLatestTagViaRedirect failed: %v", err)
	}
	if tag != "v1.7.1" {
		t.Fatalf("expected tag v1.7.1, got %q", tag)
	}
}

// TestHandleRateLimitFallbackLatestResolvesTag guards against the "vlatest" regression:
// strict mode + checksum_file + version "latest" with no explicit fallback_version must
// resolve the real tag via the redirect rather than building "v"+"latest" as the tag.
// version_expr is set so the package does not qualify for the no-API fast path and
// genuinely reaches handleRateLimitFallback.
func TestHandleRateLimitFallbackLatestResolvesTag(t *testing.T) {
	previousClient := gitRefsHTTPClient
	defer func() { gitRefsHTTPClient = previousClient }()
	gitRefsHTTPClient = redirectClient("https://github.com/owner/test-tool/releases/tag/v2.0.0")

	mgr := NewGitHubReleaseManager()
	pkg := types.Package{
		Name:         "test-tool",
		Repo:         "owner/test-tool",
		Manager:      "github_release",
		VersionExpr:  "version",
		ChecksumFile: "{{.tag}}_checksums.txt",
		AssetPatterns: map[string]string{
			"linux-amd64": "{{.name}}-{{.os}}-{{.arch}}.tar.gz",
		},
	}
	plat := platform.Platform{OS: "linux", Arch: "amd64"}

	ctx := manager.WithStrictChecksum(context.Background(), true)
	resolution, err := mgr.handleRateLimitFallback(ctx, pkg, "latest", plat, fmt.Errorf("API rate limit exceeded"))
	if err != nil {
		t.Fatalf("handleRateLimitFallback failed: %v", err)
	}
	if strings.Contains(resolution.DownloadURL, "vlatest") {
		t.Fatalf("download URL must not contain a bogus vlatest tag: %s", resolution.DownloadURL)
	}
	if !strings.Contains(resolution.DownloadURL, "v2.0.0") {
		t.Fatalf("expected resolved tag v2.0.0 in download URL, got %s", resolution.DownloadURL)
	}
	if resolution.ChecksumURL != "https://github.com/owner/test-tool/releases/download/v2.0.0/v2.0.0_checksums.txt" {
		t.Fatalf("unexpected checksum URL: %s", resolution.ChecksumURL)
	}
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
