package installer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/mock"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

type previewResolverManager struct {
	name        string
	resolved    string
	resolution  *types.Resolution
	discover    []types.Version
	resolveErr  error
	installErr  error
	remoteCalls int
}

func (m *previewResolverManager) Name() string { return m.name }

func (m *previewResolverManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	m.remoteCalls++
	return m.discover, nil
}

func (m *previewResolverManager) Resolve(ctx context.Context, pkg types.Package, version string, plat platform.Platform) (*types.Resolution, error) {
	m.remoteCalls++
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
	m.remoteCalls++
	return m.installErr
}

func (m *previewResolverManager) GetChecksums(ctx context.Context, pkg types.Package, version string) (map[string]string, error) {
	m.remoteCalls++
	return nil, nil
}

func (m *previewResolverManager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	return &types.InstalledInfo{}, nil
}

func (m *previewResolverManager) ResolveVersionConstraint(ctx context.Context, pkg types.Package, constraint string, plat platform.Platform) (string, error) {
	m.remoteCalls++
	if m.resolveErr != nil {
		return "", m.resolveErr
	}
	return m.resolved, nil
}

func writePostgRESTTestBinary(t *testing.T, binDir, version string) string {
	t.Helper()

	sourcePath, err := os.Executable()
	if err != nil {
		t.Fatalf("find test executable: %v", err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatalf("open test executable: %v", err)
	}
	defer func() { _ = source.Close() }()

	binaryName := "postgrest"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(binDir, binaryName)
	destination, err := os.OpenFile(binaryPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		t.Fatalf("create fake postgrest: %v", err)
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		t.Fatalf("copy test executable: %v", err)
	}
	if err := destination.Close(); err != nil {
		t.Fatalf("close fake postgrest: %v", err)
	}

	markerPath := filepath.Join(binDir, "postgrest-invoked")
	t.Setenv(postgrestHelperVersionEnv, version)
	t.Setenv(postgrestHelperMarkerEnv, markerPath)
	return markerPath
}

func TestInstallPostgRESTCanonicalVersionWithoutRemoteResolution(t *testing.T) {
	const managerName = "mock-postgrest-airgap"
	mgr := &previewResolverManager{
		name:       managerName,
		resolveErr: errors.New("remote version resolution invoked"),
		resolution: &types.Resolution{},
	}
	manager.GetGlobalRegistry().Register(mgr)

	defaults, err := config.LoadDefaultConfig()
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	pkg := defaults.Registry["postgrest"]
	pkg.Manager = managerName

	binDir := t.TempDir()
	markerPath := writePostgRESTTestBinary(t, binDir, "14.6")

	inst := NewWithConfig(
		&types.DepsConfig{Registry: map[string]types.Package{"postgrest": pkg}},
		WithBinDir(binDir),
		WithOS(runtime.GOOS, runtime.GOARCH),
	)
	result, err := inst.InstallWithResult("postgrest", "v14.6", &task.Task{})
	if err != nil {
		t.Fatalf("install matching local postgrest: %v", err)
	}
	if result.Status != types.InstallStatusAlreadyInstalled {
		t.Fatalf("expected already installed status, got %s", result.Status)
	}
	if result.Version.Version != "v14.6" {
		t.Fatalf("expected requested version v14.6, got %s", result.Version.Version)
	}
	if mgr.remoteCalls != 0 {
		t.Fatalf("expected no remote discovery or resolution, got %d calls", mgr.remoteCalls)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected postgrest version command to run: %v", err)
	}
}

func TestInstallFromConfigPostgRESTCanonicalVersionWithoutRemoteResolution(t *testing.T) {
	const managerName = "mock-postgrest-config-airgap"
	mgr := &previewResolverManager{
		name:       managerName,
		resolveErr: errors.New("remote version resolution invoked"),
		resolution: &types.Resolution{},
	}
	manager.GetGlobalRegistry().Register(mgr)

	defaults, err := config.LoadDefaultConfig()
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	pkg := defaults.Registry["postgrest"]
	pkg.Manager = managerName
	testConfig := &types.DepsConfig{
		Dependencies: map[string]string{"postgrest": "v14.6"},
		Registry:     map[string]types.Package{"postgrest": pkg},
	}
	previousConfig := config.GetGlobalRegistry()
	config.SetGlobalRegistry(testConfig)
	t.Cleanup(func() { config.SetGlobalRegistry(previousConfig) })

	binDir := t.TempDir()
	markerPath := writePostgRESTTestBinary(t, binDir, "14.6")
	t.Chdir(t.TempDir())
	inst := New(
		WithBinDir(binDir),
		WithOS(runtime.GOOS, runtime.GOARCH),
	)
	if err := inst.InstallFromConfig(&task.Task{}); err != nil {
		t.Fatalf("install from config: %v", err)
	}
	task.WaitForAllTasks()

	if mgr.remoteCalls != 0 {
		t.Fatalf("expected no remote discovery or resolution, got %d calls", mgr.remoteCalls)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("expected postgrest version command to run: %v", err)
	}
}

func TestInstallPostgRESTResolvesWhenLocalBinaryDoesNotMatch(t *testing.T) {
	for _, localVersion := range []string{"", "14.5"} {
		name := "absent"
		if localVersion != "" {
			name = "mismatched"
		}
		t.Run(name, func(t *testing.T) {
			managerName := "mock-postgrest-" + name
			mgr := &previewResolverManager{
				name:       managerName,
				resolveErr: errors.New("remote version resolution invoked"),
				resolution: &types.Resolution{},
			}
			manager.GetGlobalRegistry().Register(mgr)

			defaults, err := config.LoadDefaultConfig()
			if err != nil {
				t.Fatalf("load default config: %v", err)
			}
			pkg := defaults.Registry["postgrest"]
			pkg.Manager = managerName

			binDir := t.TempDir()
			if localVersion != "" {
				writePostgRESTTestBinary(t, binDir, localVersion)
			}

			inst := NewWithConfig(
				&types.DepsConfig{Registry: map[string]types.Package{"postgrest": pkg}},
				WithBinDir(binDir),
				WithOS(runtime.GOOS, runtime.GOARCH),
			)
			_, err = inst.InstallWithResult("postgrest", "v14.6", &task.Task{})
			if err == nil || !strings.Contains(err.Error(), "remote version resolution invoked") {
				t.Fatalf("expected remote resolution attempt, got %v", err)
			}
			if mgr.remoteCalls != 1 {
				t.Fatalf("expected one remote resolution call, got %d", mgr.remoteCalls)
			}
		})
	}
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
