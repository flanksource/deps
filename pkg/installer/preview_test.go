package installer

import (
	"context"
	"os"
	"testing"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/mock"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

type previewResolverManager struct {
	name       string
	resolved   string
	resolution *types.Resolution
	discover   []types.Version
	resolveErr error
	installErr error
}

func (m *previewResolverManager) Name() string { return m.name }

func (m *previewResolverManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	return m.discover, nil
}

func (m *previewResolverManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	if m.resolveErr != nil {
		return nil, m.resolveErr
	}
	resolution := *m.resolution
	resolution.Package = pkg
	resolution.Platform = plat
	resolution.Version = version
	return &resolution, nil
}

func (m *previewResolverManager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	return m.installErr
}

func (m *previewResolverManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	return nil, nil
}

func (m *previewResolverManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	return &types.InstalledInfo{}, nil
}

func (m *previewResolverManager) ResolveVersionConstraint(ctx context.Context, pkg types.Package, constraint string, plat platform.Platform) (string, error) {
	return m.resolved, nil
}

func TestPreviewUsesExistingBinaryForAny(t *testing.T) {
	const managerName = "mock-preview-any"
	manager.GetGlobalRegistry().Register(mock.NewMockPackageManager(managerName))

	tmp := t.TempDir()
	inst := NewWithConfig(
		&types.DepsConfig{
			Registry: map[string]types.Package{
				"preview-any-test-tool": {
					Name:    "preview-any-test-tool",
					Manager: managerName,
				},
			},
		},
		WithBinDir(tmp),
	)

	binPath := tmp + "/preview-any-test-tool"
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	preview, err := inst.Preview("preview-any-test-tool", "any", &task.Task{})
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	if !preview.AlreadyInstalled {
		t.Fatalf("expected already installed preview")
	}
	if preview.ExistingPath != binPath {
		t.Fatalf("expected existing path %s, got %s", binPath, preview.ExistingPath)
	}
	if preview.ExistingSource != "bin-dir" {
		t.Fatalf("expected bin-dir source, got %s", preview.ExistingSource)
	}
}

func TestPreviewNormalizesEffectiveVersionFromResolution(t *testing.T) {
	const managerName = "mock-preview-effective"
	manager.GetGlobalRegistry().Register(&previewResolverManager{
		name:     managerName,
		resolved: "stable",
		resolution: &types.Resolution{
			DownloadURL: "file:///tmp/mock-preview-effective",
			GitHubAsset: &types.GitHubAsset{
				Repo: "owner/repo",
				Tag:  "v1.2.3",
			},
		},
	})

	inst := NewWithConfig(&types.DepsConfig{
		Registry: map[string]types.Package{
			"preview-effective-test-tool": {
				Name:    "preview-effective-test-tool",
				Manager: managerName,
			},
		},
	})

	preview, err := inst.Preview("preview-effective-test-tool", "latest", &task.Task{})
	if err != nil {
		t.Fatalf("preview failed: %v", err)
	}

	if preview.RequestedVersion != "latest" {
		t.Fatalf("expected requested version latest, got %s", preview.RequestedVersion)
	}
	if preview.ResolvedVersion != "stable" {
		t.Fatalf("expected resolved version stable, got %s", preview.ResolvedVersion)
	}
	if preview.EffectiveVersion != "1.2.3" {
		t.Fatalf("expected effective version 1.2.3, got %s", preview.EffectiveVersion)
	}
}
