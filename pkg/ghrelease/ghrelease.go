// Package ghrelease installs one named binary from a GitHub release with only
// the standard library plus an xz decoder. It exists for callers (such as a
// database test harness) that need a single tool and cannot afford the full
// registry installer, whose package graph pulls in the CLI framework,
// Kubernetes API types and the template engine.
//
// The caller supplies the release layout (repository, per-platform asset
// names) explicitly; nothing is read from the deps registry.
package ghrelease

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultBaseURL = "https://github.com"

// Request describes one binary to install.
type Request struct {
	// Repo is the GitHub "owner/name".
	Repo string
	// Version is a release tag ("v1.2.3" or "1.2.3", a leading "v" is added
	// when missing) or "latest".
	Version string
	// Binary is the executable's file name inside the archive, without ".exe".
	Binary string
	// Assets maps "<goos>-<goarch>" to the release asset name. "{{.tag}}" is
	// replaced with the resolved tag.
	Assets map[string]string
	// BinDir is where the binary is installed.
	BinDir string
	// BaseURL overrides https://github.com (tests, GitHub Enterprise).
	BaseURL string
	// Client overrides http.DefaultClient.
	Client *http.Client
}

// Install installs the binary into BinDir and returns its path. An existing
// binary is reused only when its version stamp (written next to the binary on
// install) matches the requested repo, resolved tag and asset; otherwise it is
// replaced atomically.
func Install(ctx context.Context, req Request) (string, error) {
	if err := req.validate(); err != nil {
		return "", err
	}
	target := filepath.Join(req.BinDir, req.Binary+exeSuffix())
	pattern, ok := req.Assets[runtime.GOOS+"-"+runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("ghrelease: no asset for %s-%s in %s", runtime.GOOS, runtime.GOARCH, req.Repo)
	}
	tag, err := req.resolveTag(ctx)
	if err != nil {
		return "", err
	}
	asset := strings.ReplaceAll(pattern, "{{.tag}}", tag)
	stamp := req.stamp(tag, asset)
	stampPath := filepath.Join(req.BinDir, "."+req.Binary+".ghrelease")
	if current, err := installedMatches(target, stampPath, stamp); err != nil {
		return "", err
	} else if current {
		return target, nil
	}
	archive, err := req.download(ctx, tag, asset)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(archive) }()
	if err := os.MkdirAll(req.BinDir, 0o755); err != nil {
		return "", fmt.Errorf("ghrelease: create %s: %w", req.BinDir, err)
	}
	if err := extractBinary(archive, asset, req.Binary+exeSuffix(), target); err != nil {
		return "", err
	}
	if err := os.WriteFile(stampPath, []byte(stamp), 0o644); err != nil {
		return "", fmt.Errorf("ghrelease: write stamp %s: %w", stampPath, err)
	}
	return target, nil
}

func (r Request) stamp(tag, asset string) string {
	return r.Repo + "\n" + tag + "\n" + asset + "\n"
}

// installedMatches reports whether target exists and stampPath holds want. A
// missing binary or stamp means "not current"; any other read error fails.
func installedMatches(target, stampPath, want string) (bool, error) {
	info, err := os.Stat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ghrelease: stat %s: %w", target, err)
	}
	if info.IsDir() {
		return false, nil
	}
	got, err := os.ReadFile(stampPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ghrelease: read stamp %s: %w", stampPath, err)
	}
	return string(got) == want, nil
}

func (r Request) validate() error {
	switch {
	case r.Repo == "":
		return fmt.Errorf("ghrelease: Repo is required")
	case r.Binary == "":
		return fmt.Errorf("ghrelease: Binary is required")
	case r.Version == "":
		return fmt.Errorf("ghrelease: Version is required (use \"latest\" explicitly)")
	case r.BinDir == "":
		return fmt.Errorf("ghrelease: BinDir is required")
	}
	return nil
}

func (r Request) baseURL() string {
	if r.BaseURL != "" {
		return strings.TrimRight(r.BaseURL, "/")
	}
	return defaultBaseURL
}

func (r Request) httpClient() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return http.DefaultClient
}

func (r Request) resolveTag(ctx context.Context) (string, error) {
	if r.Version != "latest" {
		return "v" + strings.TrimPrefix(r.Version, "v"), nil
	}
	client := *r.httpClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL()+"/"+r.Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ghrelease: resolve latest %s: %w", r.Repo, err)
	}
	defer func() { _ = resp.Body.Close() }()
	location, err := resp.Location()
	if err != nil {
		return "", fmt.Errorf("ghrelease: resolve latest %s: status %d without redirect: %w", r.Repo, resp.StatusCode, err)
	}
	tag := path.Base(location.Path)
	if !strings.Contains(location.Path, "/releases/tag/") {
		return "", fmt.Errorf("ghrelease: resolve latest %s: unexpected redirect to %s", r.Repo, location)
	}
	return tag, nil
}

func (r Request) download(ctx context.Context, tag, asset string) (string, error) {
	assetURL := r.baseURL() + "/" + r.Repo + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("ghrelease: download %s: %w", assetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ghrelease: download %s: HTTP %d %s", assetURL, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	tmp, err := os.CreateTemp("", "ghrelease-*"+archiveExt(asset))
	if err != nil {
		return "", err
	}
	defer func() { _ = tmp.Close() }()
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("ghrelease: download %s: %w", assetURL, err)
	}
	return tmp.Name(), nil
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
