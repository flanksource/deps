package start

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/deps/pkg/download"
	"github.com/flanksource/deps/pkg/extract"
	"gopkg.in/yaml.v3"
)

func prepareHelmChart(ctx context.Context, svc *ServiceContext) (string, error) {
	source := svc.Spec.Helm.Source
	checksum, err := hex.DecodeString(source.SHA256)
	if err != nil || len(checksum) != 32 {
		return "", fmt.Errorf("helm source requires a SHA-256 checksum")
	}
	if svc.Spec.Helm.Repo != "" || source.URL == "" || !filepath.IsLocal(source.Path) {
		return "", fmt.Errorf("helm source requires a URL and local chart path, and cannot use repo")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	archive := filepath.Join(svc.RunDir, "chart-source.tar.gz")
	if err := download.Download(source.URL, archive, nil, download.WithChecksum("sha256:"+source.SHA256), download.WithTimeout(svc.Opts.WaitTimeout)); err != nil {
		return "", fmt.Errorf("download helm source: %w", err)
	}
	root := filepath.Join(svc.RunDir, "chart-source")
	if _, err := extract.Extract(archive, root, nil, extract.WithFullExtract()); err != nil {
		return "", err
	}
	chart, err := filepath.EvalSymlinks(filepath.Join(root, source.Path))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, chart)
	if err != nil || strings.HasPrefix(relative, "..") {
		return "", fmt.Errorf("helm chart escapes source directory")
	}
	content, err := os.ReadFile(filepath.Join(chart, "Chart.yaml"))
	if err != nil {
		return "", err
	}
	var metadata struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(content, &metadata); err != nil {
		return "", err
	}
	if metadata.Name != svc.Spec.Helm.Chart || metadata.Version != svc.Spec.Helm.ChartVersion {
		return "", fmt.Errorf("helm source chart %s@%s does not match %s@%s", metadata.Name, metadata.Version, svc.Spec.Helm.Chart, svc.Spec.Helm.ChartVersion)
	}
	return chart, nil
}
