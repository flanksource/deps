package version

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CheckExistingInstallation for directory-mode symlinks", func() {
	It("reads the version from a launcher that sources siblings relative to its own path", func() {
		if runtime.GOOS == "windows" {
			Skip("launcher is a shell script")
		}
		const installed = "9.5.5"
		root := GinkgoT().TempDir()
		appBin := filepath.Join(root, "app", "launcher", "bin")
		binDir := filepath.Join(root, "bin")
		Expect(os.MkdirAll(appBin, 0o755)).To(Succeed())
		Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())

		// Mirrors bin/elasticsearch, which fails unless $0 is inside the app directory.
		Expect(os.WriteFile(filepath.Join(appBin, "launcher-env"), []byte("VERSION="+installed+"\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(appBin, "launcher"),
			[]byte("#!/bin/sh\n. \"$(dirname \"$0\")/launcher-env\"\necho \"Version: $VERSION\"\n"), 0o755)).To(Succeed())
		Expect(os.Symlink(filepath.Join(appBin, "launcher"), filepath.Join(binDir, "launcher"))).To(Succeed())

		pkg := types.Package{
			Name:           "launcher",
			Mode:           "directory",
			Symlinks:       []string{"bin/launcher"},
			VersionCommand: "bin/launcher --version",
			VersionRegex:   `Version: (\d+\.\d+\.\d+)`,
		}
		Expect(CheckExistingInstallation(&task.Task{}, "launcher", pkg, installed, binDir, "")).To(Equal(installed))
	})

	It("runs a shell version command inside the app directory the bin-dir link points into", func() {
		if runtime.GOOS == "windows" {
			Skip("launcher is a shell script")
		}
		const installed = "3.9.0"
		root := GinkgoT().TempDir()
		appDir := filepath.Join(root, "app", "launcher")
		binDir := filepath.Join(root, "bin")
		Expect(os.MkdirAll(filepath.Join(appDir, "bin"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(appDir, "jdk"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(binDir, 0o755)).To(Succeed())

		// Mirrors bin/opensearch, which needs a JDK selected relative to the app directory.
		Expect(os.WriteFile(filepath.Join(appDir, "bin", "launcher"),
			[]byte("#!/bin/sh\n[ -d \"$LAUNCHER_JAVA_HOME\" ] || exit 1\necho \"Version: "+installed+"\"\n"), 0o755)).To(Succeed())
		Expect(os.Symlink(filepath.Join(appDir, "bin", "launcher"), filepath.Join(binDir, "launcher"))).To(Succeed())

		pkg := types.Package{
			Name:           "launcher",
			Mode:           "directory",
			Symlinks:       []string{"bin/launcher"},
			VersionCommand: `bash -c 'LAUNCHER_JAVA_HOME="$PWD/jdk" bin/launcher --version 2>&1'`,
			VersionRegex:   `Version: (\d+\.\d+\.\d+)`,
		}
		Expect(CheckExistingInstallation(&task.Task{}, "launcher", pkg, installed, binDir, "")).To(Equal(installed))
	})
})
