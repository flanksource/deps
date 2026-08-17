package installer

import (
	"os"
	"path/filepath"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// writeVersionStub installs a runnable stub that reports version for any argument, so the
// existing-installation check has something real to execute.
func writeVersionStub(binDir, name, version string) string {
	path := filepath.Join(binDir, name)
	Expect(os.WriteFile(path, []byte("#!/bin/sh\necho "+version+"\n"), 0755)).To(Succeed())
	return path
}

// registerPreviewManager registers a stub manager that mimics the github_release fast path:
// ResolveVersionConstraint hands the alias straight back, and only Resolve knows the concrete
// version behind it.
func registerPreviewManager(name, resolved string, resolution *types.Resolution) *previewResolverManager {
	mgr := &previewResolverManager{name: name, resolved: resolved, resolution: resolution}
	manager.GetGlobalRegistry().Register(mgr)
	return mgr
}

func previewInstaller(managerName, pkgName, binDir string) *Installer {
	return NewWithConfig(&types.DepsConfig{
		Registry: map[string]types.Package{
			pkgName: {Name: pkgName, Manager: managerName},
		},
	}, WithBinDir(binDir))
}

var _ = Describe("preview of an alias version", func() {
	var binDir string

	BeforeEach(func() {
		binDir = GinkgoT().TempDir()
	})

	It("skips the install when latest resolves to the installed version via the release tag", func() {
		const pkgName = "preview-alias-tag-tool"
		registerPreviewManager("mock-preview-alias-tag", "latest", &types.Resolution{
			DownloadURL: "file:///dev/null",
			GitHubAsset: &types.GitHubAsset{Repo: "owner/repo", Tag: "v3.52.0"},
		})
		binPath := writeVersionStub(binDir, pkgName, "3.52.0")

		preview, err := previewInstaller("mock-preview-alias-tag", pkgName, binDir).Preview(pkgName, "latest", &task.Task{})

		Expect(err).ToNot(HaveOccurred())
		Expect(preview.AlreadyInstalled).To(BeTrue())
		Expect(preview.ExistingVersion).To(Equal("3.52.0"))
		Expect(preview.ExistingPath).To(Equal(binPath))
		Expect(preview.EffectiveVersion).To(Equal("3.52.0"))
	})

	It("skips the install when latest resolves to the installed version without asset metadata", func() {
		// The no-API github_release path (checksum_file + deterministic asset pattern) reports
		// the concrete version on the resolution and leaves GitHubAsset nil.
		const pkgName = "preview-alias-version-tool"
		registerPreviewManager("mock-preview-alias-version", "latest", &types.Resolution{
			Version:     "3.52.0",
			DownloadURL: "file:///dev/null",
		})
		binPath := writeVersionStub(binDir, pkgName, "3.52.0")

		preview, err := previewInstaller("mock-preview-alias-version", pkgName, binDir).Preview(pkgName, "latest", &task.Task{})

		Expect(err).ToNot(HaveOccurred())
		Expect(preview.AlreadyInstalled).To(BeTrue())
		Expect(preview.ExistingVersion).To(Equal("3.52.0"))
		Expect(preview.ExistingPath).To(Equal(binPath))
		Expect(preview.EffectiveVersion).To(Equal("3.52.0"))
	})

	It("installs when latest resolves past the installed version", func() {
		const pkgName = "preview-alias-outdated-tool"
		registerPreviewManager("mock-preview-alias-outdated", "latest", &types.Resolution{
			DownloadURL: "file:///dev/null",
			GitHubAsset: &types.GitHubAsset{Repo: "owner/repo", Tag: "v3.52.0"},
		})
		writeVersionStub(binDir, pkgName, "3.51.0")

		preview, err := previewInstaller("mock-preview-alias-outdated", pkgName, binDir).Preview(pkgName, "latest", &task.Task{})

		Expect(err).ToNot(HaveOccurred())
		Expect(preview.AlreadyInstalled).To(BeFalse())
		Expect(preview.EffectiveVersion).To(Equal("3.52.0"))
	})

	It("short-circuits a pinned version without resolving", func() {
		const pkgName = "preview-pinned-tool"
		mgr := registerPreviewManager("mock-preview-pinned", "3.52.0", &types.Resolution{
			DownloadURL: "file:///dev/null",
		})
		writeVersionStub(binDir, pkgName, "3.52.0")

		preview, err := previewInstaller("mock-preview-pinned", pkgName, binDir).Preview(pkgName, "3.52.0", &task.Task{})

		Expect(err).ToNot(HaveOccurred())
		Expect(preview.AlreadyInstalled).To(BeTrue())
		Expect(preview.ExistingVersion).To(Equal("3.52.0"))
		Expect(mgr.resolveCall).To(Equal(0), "a pinned version that is already installed must not hit the network")
	})
})
