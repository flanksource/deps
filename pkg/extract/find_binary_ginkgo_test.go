package extract

import (
	"os"
	"path/filepath"

	"github.com/flanksource/clicky/task"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FindBinaryInDir", func() {
	It("finds a Windows executable extracted without an exec bit", func() {
		// zip archives built on Windows carry no unix mode, so a .exe unpacks as 0644
		dir := GinkgoT().TempDir()
		exe := filepath.Join(dir, "gitlab-runner-windows-amd64.exe")
		Expect(os.WriteFile(exe, []byte("MZ"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "README.md"), []byte("docs"), 0o644)).To(Succeed())

		Expect(FindBinaryInDir(dir, "", &task.Task{})).To(Equal(exe))
	})
})
