package installer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	elfHeader   = append([]byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01}, make([]byte, 57)...)
	machoHeader = append([]byte{0xcf, 0xfa, 0xed, 0xfe, 0x0c, 0x00, 0x00, 0x01}, make([]byte, 56)...)
	peHeader    = append([]byte{'M', 'Z', 0x90, 0x00}, make([]byte, 60)...)

	// The exact payload deps installed as its own binary: the contents of
	// deps-linux-amd64.sha256 from the flanksource/deps v1.0.39 release.
	checksumFile = []byte("04002fd93c97ff6c234853bd504efc79861ce5de43876d9f6949cfae9b0e9f81  deps-linux-amd64\n")
)

var _ = Describe("VerifyExecutable", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	write := func(name string, content []byte) string {
		path := filepath.Join(dir, name)
		Expect(os.WriteFile(path, content, 0755)).To(Succeed())
		return path
	}

	It("rejects a checksum file installed in place of a binary", func() {
		err := VerifyExecutable(write("deps", checksumFile), "deps-linux-amd64.sha256",
			platform.Platform{OS: "linux", Arch: "amd64"})

		Expect(err).To(MatchError(ContainSubstring("deps-linux-amd64.sha256 is not an executable")))
		Expect(err).To(MatchError(ContainSubstring("linux-amd64 expects ELF")))
		Expect(err).To(MatchError(ContainSubstring("04002fd93c97ff6c")))
	})

	It("rejects an empty file", func() {
		err := VerifyExecutable(write("tool", nil), "tool-linux-amd64",
			platform.Platform{OS: "linux", Arch: "amd64"})

		Expect(err).To(MatchError(ContainSubstring("tool-linux-amd64 is empty")))
	})

	It("rejects an HTML error page saved as a binary", func() {
		err := VerifyExecutable(write("tool", []byte("<!DOCTYPE html>\n<html><body>404</body></html>")), "tool-linux-amd64",
			platform.Platform{OS: "linux", Arch: "amd64"})

		Expect(err).To(MatchError(ContainSubstring("<!DOCTYPE html>")))
	})

	It("reports a binary built for the wrong platform", func() {
		err := VerifyExecutable(write("tool", machoHeader), "tool-darwin-arm64",
			platform.Platform{OS: "linux", Arch: "amd64"})

		Expect(err).To(MatchError(ContainSubstring("is a Mach-O binary, but linux-amd64 needs ELF")))
	})

	DescribeTable("accepts runnable artifacts",
		func(content []byte, plat platform.Platform) {
			Expect(VerifyExecutable(write("tool", content), "tool", plat)).To(Succeed())
		},
		Entry("ELF on linux", elfHeader, platform.Platform{OS: "linux", Arch: "amd64"}),
		Entry("Mach-O on darwin", machoHeader, platform.Platform{OS: "darwin", Arch: "arm64"}),
		Entry("PE on windows", peHeader, platform.Platform{OS: "windows", Arch: "amd64"}),
		Entry("shebang script on any platform", []byte("#!/usr/bin/env bash\necho hi\n"), platform.Platform{OS: "linux", Arch: "amd64"}),
	)

	It("ignores directories, which are validated by their symlinks", func() {
		Expect(VerifyExecutable(dir, "tool", platform.Platform{OS: "linux", Arch: "amd64"})).To(Succeed())
	})
})

var _ = Describe("installing a checksum file in place of a binary", func() {
	It("fails the install and leaves nothing behind at the target path", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(checksumFile)
		}))
		DeferCleanup(server.Close)

		installDir := GinkgoT().TempDir()
		pkg := types.Package{Name: "deps", Repo: "flanksource/deps", Manager: "github_release"}
		preview := &InstallPreview{
			EffectiveVersion: "1.0.39",
			Resolution: &types.Resolution{
				Package: pkg, Version: "1.0.39",
				Platform:    platform.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
				DownloadURL: server.URL,
				GitHubAsset: &types.GitHubAsset{AssetName: "deps-linux-amd64.sha256"},
			},
		}

		err := New(
			WithBinDir(installDir),
			WithTmpDir(installDir),
			WithSkipChecksum(true),
		).executePackageInstallation(context.Background(), "deps", pkg, preview, nil, &task.Task{}, nil)

		Expect(err).To(MatchError(ContainSubstring("deps: installed asset deps-linux-amd64.sha256 is not an executable")))
		Expect(filepath.Join(installDir, "deps")).ToNot(BeAnExistingFile())
	})
})
