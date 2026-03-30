package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/flanksource/deps/mock"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/types"
	"github.com/spf13/cobra"
)

func TestRunInfoIncludesInstallPreview(t *testing.T) {
	const managerName = "mock-info-preview"
	manager.GetGlobalRegistry().Register(
		mock.NewMockPackageManager(managerName).WithVersions("1.2.0", "1.1.0"),
	)

	prevConfig := depsConfig
	prevBinDir := binDir
	prevAppDir := appDir
	prevTmpDir := tmpDir
	prevCacheDir := cacheDir
	prevForce := force
	prevSkipChecksum := skipChecksum
	prevStrictChecksum := strictChecksum
	prevDebug := debug
	prevOSOverride := osOverride
	prevArchOverride := archOverride
	prevTimeout := timeout
	prevIterateVersions := iterateVersions
	prevInfoAll := infoAll
	prevInfoAllLatest := infoAllLatest
	prevInfoVersionLimit := infoVersionLimit
	defer func() {
		depsConfig = prevConfig
		binDir = prevBinDir
		appDir = prevAppDir
		tmpDir = prevTmpDir
		cacheDir = prevCacheDir
		force = prevForce
		skipChecksum = prevSkipChecksum
		strictChecksum = prevStrictChecksum
		debug = prevDebug
		osOverride = prevOSOverride
		archOverride = prevArchOverride
		timeout = prevTimeout
		iterateVersions = prevIterateVersions
		infoAll = prevInfoAll
		infoAllLatest = prevInfoAllLatest
		infoVersionLimit = prevInfoVersionLimit
	}()

	tmp := t.TempDir()
	depsConfig = &types.DepsConfig{
		Registry: map[string]types.Package{
			"info-preview-test-tool": {
				Name:    "info-preview-test-tool",
				Manager: managerName,
			},
		},
	}
	binDir = tmp
	appDir = tmp
	tmpDir = tmp
	cacheDir = ""
	force = false
	skipChecksum = false
	strictChecksum = true
	debug = false
	osOverride = ""
	archOverride = ""
	iterateVersions = 0
	infoAll = false
	infoAllLatest = false
	infoVersionLimit = 10

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	if err := runInfo(cmd, []string{"info-preview-test-tool@latest"}); err != nil {
		t.Fatalf("runInfo failed: %v", err)
	}

	output := out.String()
	for _, expected := range []string{
		"Package: info-preview-test-tool",
		"Available Versions",
		"1.2.0",
		"Install Preview for",
		"Requested: latest",
		"Resolved Version: v1.2.0",
		"Method: download",
		"URL: file:///tmp/mock-info-preview-test-tool-v1.2.0-",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected output to contain %q, got:\n%s", expected, output)
		}
	}
}
