package extract

import (
	"archive/tar"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Git source archives", func() {
	It("extracts files following a global PAX metadata header", func() {
		root := GinkgoT().TempDir()
		archive := filepath.Join(root, "source.tar")
		file, err := os.Create(archive)
		Expect(err).NotTo(HaveOccurred())
		writer := tar.NewWriter(file)
		Expect(writer.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": "source-commit"}})).To(Succeed())
		payload := "name: fixture-chart\n"
		Expect(writer.WriteHeader(&tar.Header{Name: "chart/Chart.yaml", Mode: 0600, Size: int64(len(payload))})).To(Succeed())
		_, err = writer.Write([]byte(payload))
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		Expect(file.Close()).To(Succeed())
		_, err = Unarchive(archive, filepath.Join(root, "output"))
		Expect(err).NotTo(HaveOccurred())
		content, err := os.ReadFile(filepath.Join(root, "output/chart/Chart.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(payload))
		Expect(filepath.Join(root, "output/pax_global_header")).NotTo(BeAnExistingFile())
	})
})
