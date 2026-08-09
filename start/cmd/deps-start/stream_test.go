package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("prefixWriter", func() {
	It("prefixes each complete line and holds back a partial one", func() {
		var out bytes.Buffer
		writer := newPrefixWriter(&out, "postgres", false)

		_, err := writer.Write([]byte("LOG:  starting\nLOG:  listen"))
		Expect(err).ToNot(HaveOccurred())
		Expect(out.String()).To(Equal("postgres │ LOG:  starting\n"))

		_, err = writer.Write([]byte("ing on 5432\n"))
		Expect(err).ToNot(HaveOccurred())
		Expect(out.String()).To(Equal("postgres │ LOG:  starting\npostgres │ LOG:  listening on 5432\n"))
	})

	It("flushes a trailing line that never got a newline", func() {
		var out bytes.Buffer
		writer := newPrefixWriter(&out, "valkey", false)

		_, err := writer.Write([]byte("Ready to accept connections"))
		Expect(err).ToNot(HaveOccurred())
		Expect(out.String()).To(BeEmpty())

		writer.Flush()
		Expect(out.String()).To(Equal("valkey │ Ready to accept connections\n"))
	})

	It("strips carriage returns so CRLF output stays on one line", func() {
		var out bytes.Buffer
		writer := newPrefixWriter(&out, "nats", false)

		_, err := writer.Write([]byte("listening\r\n"))
		Expect(err).ToNot(HaveOccurred())
		Expect(out.String()).To(Equal("nats │ listening\n"))
	})

	It("colours the prefix only when writing to a terminal", func() {
		var plain, ansi bytes.Buffer
		_, err := newPrefixWriter(&plain, "postgres", false).Write([]byte("x\n"))
		Expect(err).ToNot(HaveOccurred())
		_, err = newPrefixWriter(&ansi, "postgres", true).Write([]byte("x\n"))
		Expect(err).ToNot(HaveOccurred())

		Expect(plain.String()).ToNot(ContainSubstring("\x1b["))
		Expect(ansi.String()).To(ContainSubstring("\x1b["))
	})
})

var _ = Describe("portsLabel", func() {
	It("renders ports as URLs terminals can linkify", func() {
		Expect(portsLabel([]int{5432, 9200})).To(Equal("http://localhost:5432 http://localhost:9200"))
	})

	It("is empty when no ports are known", func() {
		Expect(portsLabel(nil)).To(BeEmpty())
	})
})

var _ = Describe("log tailing", func() {
	It("shows only output appended after the tail was recorded", func() {
		path := filepath.Join(GinkgoT().TempDir(), "service.log")
		Expect(os.WriteFile(path, []byte("from an earlier run\n"), 0o644)).To(Succeed())

		tail := logTailFrom(path)
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(file.Close)
		_, err = file.WriteString("from this run\n")
		Expect(err).ToNot(HaveOccurred())

		var out bytes.Buffer
		writer := newPrefixWriter(&out, "postgres", false)
		ctx, cancel := context.WithCancel(context.Background())
		tailLogsUntil(ctx, writer, tail)
		Eventually(out.String).Should(Equal("postgres │ from this run\n"))
		cancel()
	})

	It("waits for a log file the supervisor has not created yet", func() {
		path := filepath.Join(GinkgoT().TempDir(), "service.log")
		tail := logTailFrom(path)

		var out bytes.Buffer
		writer := newPrefixWriter(&out, "postgres", false)
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)
		tailLogsUntil(ctx, writer, tail)

		time.Sleep(50 * time.Millisecond)
		Expect(os.WriteFile(path, []byte("started\n"), 0o644)).To(Succeed())
		Eventually(out.String, time.Second).Should(Equal("postgres │ started\n"))
	})
})
