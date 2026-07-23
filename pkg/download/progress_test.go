package download

import (
	"bytes"
	"time"

	"github.com/flanksource/clicky/task"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("download progress", func() {
	It("keeps the selected asset name in progress descriptions", func() {
		t := &task.Task{}
		reader := &ProgressReader{
			Reader:      bytes.NewReader([]byte("payload")),
			total:       7,
			task:        t,
			displayName: "faro_darwin_amd64",
			startTime:   time.Now().Add(-time.Second),
		}

		_, err := reader.Read(make([]byte, 7))
		Expect(err).NotTo(HaveOccurred())
		Expect(t.Description()).To(ContainSubstring("Downloading faro_darwin_amd64:"))
	})
})
