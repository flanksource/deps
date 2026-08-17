package installer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("asset override installation", func() {
	It("uses the derived binary name and shows the asset while downloading", func() {
		// A shebang script is a runnable artifact on every host, so the install passes
		// the executable check without needing per-platform object file fixtures.
		payload := []byte("#!/bin/sh\necho payload\n")
		requestStarted := make(chan struct{})
		releaseResponse := make(chan struct{})
		var releaseOnce sync.Once
		DeferCleanup(func() { releaseOnce.Do(func() { close(releaseResponse) }) })
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			close(requestStarted)
			<-releaseResponse
			_, _ = w.Write(payload)
		}))
		DeferCleanup(server.Close)

		installDir := GinkgoT().TempDir()
		pkg := types.Package{Name: "mission-control", Repo: "owner/repo", Manager: "github_release"}
		preview := &InstallPreview{
			EffectiveVersion: "0.0.1887",
			Resolution: &types.Resolution{
				Package: pkg, Version: "0.0.1887",
				Platform:    platform.Platform{OS: "darwin", Arch: "amd64"},
				DownloadURL: server.URL,
				GitHubAsset: &types.GitHubAsset{AssetName: "faro_darwin_amd64"},
			},
		}
		t := &task.Task{}
		done := make(chan error, 1)
		go func() {
			done <- New(
				WithBinDir(installDir),
				WithTmpDir(installDir),
				WithAssetFilters("faro"),
				WithSkipChecksum(true),
			).executePackageInstallation(context.Background(), "mission-control", pkg, preview, nil, t, nil)
		}()

		<-requestStarted
		Eventually(t.Name).Should(Equal("faro@0.0.1887"))
		Eventually(t.Description).Should(ContainSubstring("Downloading faro_darwin_amd64"))
		releaseOnce.Do(func() { close(releaseResponse) })
		Expect(<-done).To(Succeed())
		content, err := os.ReadFile(filepath.Join(installDir, "faro"))
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(Equal(payload))
	})
})
