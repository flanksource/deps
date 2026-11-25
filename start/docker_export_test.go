package start

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Docker file exports", func() {
	It("writes private files and preserves the last complete export after a truncated copy", func() {
		dir := GinkgoT().TempDir()
		name := "client-config"
		contents := "emulator client configuration\n"
		var archive bytes.Buffer
		writer := tar.NewWriter(&archive)
		Expect(writer.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(contents))})).To(Succeed())
		truncatedSize := archive.Len() + len(contents) - 1
		_, err := writer.Write([]byte(contents))
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())

		Expect(writeDockerExport(dir, name, bytes.NewReader(archive.Bytes()))).To(Succeed())
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		if runtime.GOOS != "windows" {
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		}

		Expect(writeDockerExport(dir, name, bytes.NewReader(archive.Bytes()[:truncatedSize]))).To(MatchError("unexpected EOF"))
		Expect(os.ReadFile(path)).To(Equal([]byte(contents)))
	})
})
