package ghrelease

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	repo    = "acme/tool"
	payload = "#!/bin/sh\necho tool\n"
)

func tarGz(name, content string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	Expect(tw.WriteHeader(&tar.Header{Name: "tool-dist/" + name, Mode: 0o755, Size: int64(len(content))})).To(Succeed())
	_, err := tw.Write([]byte(content))
	Expect(err).ToNot(HaveOccurred())
	Expect(tw.Close()).To(Succeed())
	Expect(gz.Close()).To(Succeed())
	return buf.Bytes()
}

func zipFile(name, content string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	Expect(err).ToNot(HaveOccurred())
	_, err = w.Write([]byte(content))
	Expect(err).ToNot(HaveOccurred())
	Expect(zw.Close()).To(Succeed())
	return buf.Bytes()
}

var _ = Describe("Install", func() {
	var (
		server  *httptest.Server
		assets  map[string][]byte
		latest  string
		request Request
	)
	platformKey := runtime.GOOS + "-" + runtime.GOARCH

	BeforeEach(func() {
		assets, latest = map[string][]byte{}, "v2.0.0"
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/"+repo+"/releases/latest" {
				http.Redirect(w, r, "/"+repo+"/releases/tag/"+latest, http.StatusFound)
				return
			}
			body, ok := assets[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		}))
		DeferCleanup(server.Close)
		request = Request{
			Repo: repo, Binary: "tool", BinDir: GinkgoT().TempDir(), BaseURL: server.URL,
			Assets: map[string]string{platformKey: "tool-{{.tag}}.tar.gz"},
		}
	})

	It("downloads the pinned tag's asset and extracts the binary from a nested archive path", func() {
		assets["/"+repo+"/releases/download/v1.2.3/tool-v1.2.3.tar.gz"] = tarGz("tool", payload)
		request.Version = "1.2.3"

		path, err := Install(context.Background(), request)

		Expect(err).ToNot(HaveOccurred())
		Expect(path).To(Equal(filepath.Join(request.BinDir, "tool")))
		Expect(os.ReadFile(path)).To(Equal([]byte(payload)))
		info, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm() & 0o111).ToNot(BeZero())
	})

	It("resolves latest through the releases redirect", func() {
		assets["/"+repo+"/releases/download/v2.0.0/tool-v2.0.0.tar.gz"] = tarGz("tool", payload)
		request.Version = "latest"

		path, err := Install(context.Background(), request)

		Expect(err).ToNot(HaveOccurred())
		Expect(os.ReadFile(path)).To(Equal([]byte(payload)))
	})

	It("extracts from a zip asset", func() {
		request.Assets = map[string]string{platformKey: "tool-{{.tag}}.zip"}
		assets["/"+repo+"/releases/download/v1.0.0/tool-v1.0.0.zip"] = zipFile("bin/tool", payload)
		request.Version = "v1.0.0"

		path, err := Install(context.Background(), request)

		Expect(err).ToNot(HaveOccurred())
		Expect(os.ReadFile(path)).To(Equal([]byte(payload)))
	})

	Describe("existing binary", func() {
		const oldBinary = "old"
		var existing, stampPath string

		stampFor := func(tag string) string {
			return repo + "\n" + tag + "\ntool-" + tag + ".tar.gz\n"
		}
		seed := func(stamp *string) {
			existing = filepath.Join(request.BinDir, "tool")
			stampPath = filepath.Join(request.BinDir, ".tool.ghrelease")
			Expect(os.WriteFile(existing, []byte(oldBinary), 0o755)).To(Succeed())
			if stamp != nil {
				Expect(os.WriteFile(stampPath, []byte(*stamp), 0o644)).To(Succeed())
			}
		}

		It("is reused without downloading when the stamp matches the pinned version", func() {
			stamp := stampFor("v1.2.3")
			seed(&stamp)
			request.Version = "1.2.3"

			path, err := Install(context.Background(), request)

			Expect(err).ToNot(HaveOccurred())
			Expect(os.ReadFile(path)).To(Equal([]byte(oldBinary)))
		})

		It("is replaced when it has no stamp", func() {
			seed(nil)
			assets["/"+repo+"/releases/download/v1.2.3/tool-v1.2.3.tar.gz"] = tarGz("tool", payload)
			request.Version = "1.2.3"

			path, err := Install(context.Background(), request)

			Expect(err).ToNot(HaveOccurred())
			Expect(os.ReadFile(path)).To(Equal([]byte(payload)))
			Expect(os.ReadFile(stampPath)).To(Equal([]byte(stampFor("v1.2.3"))))
		})

		It("is replaced and restamped when the stamp is for another version", func() {
			stamp := stampFor("v1.0.0")
			seed(&stamp)
			assets["/"+repo+"/releases/download/v1.2.3/tool-v1.2.3.tar.gz"] = tarGz("tool", payload)
			request.Version = "1.2.3"

			path, err := Install(context.Background(), request)

			Expect(err).ToNot(HaveOccurred())
			Expect(os.ReadFile(path)).To(Equal([]byte(payload)))
			Expect(os.ReadFile(stampPath)).To(Equal([]byte(stampFor("v1.2.3"))))
		})

		It("resolves latest first, reusing when the stamp matches and replacing when it does not", func() {
			stamp := stampFor("v2.0.0")
			seed(&stamp)
			request.Version = "latest"

			path, err := Install(context.Background(), request)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.ReadFile(path)).To(Equal([]byte(oldBinary)))

			latest = "v3.0.0"
			assets["/"+repo+"/releases/download/v3.0.0/tool-v3.0.0.tar.gz"] = tarGz("tool", payload)
			path, err = Install(context.Background(), request)
			Expect(err).ToNot(HaveOccurred())
			Expect(os.ReadFile(path)).To(Equal([]byte(payload)))
			Expect(os.ReadFile(stampPath)).To(Equal([]byte(stampFor("v3.0.0"))))
		})

		It("fails on a stamp read error other than not-exist", func() {
			seed(nil)
			Expect(os.Mkdir(stampPath, 0o755)).To(Succeed())
			request.Version = "1.2.3"

			_, err := Install(context.Background(), request)

			Expect(err).To(MatchError(ContainSubstring("read stamp")))
		})
	})

	DescribeTable("fails loudly",
		func(mutate func(*Request), want string) {
			request.Version = "1.2.3"
			mutate(&request)
			_, err := Install(context.Background(), request)
			Expect(err).To(MatchError(ContainSubstring(want)))
		},
		Entry("on a platform with no asset pattern", func(r *Request) { r.Assets = map[string]string{"plan9-mips": "x"} }, "no asset for "+platformKey),
		Entry("when the release asset is missing", func(r *Request) {}, "404"),
		Entry("without a bin dir", func(r *Request) { r.BinDir = "" }, "BinDir"),
	)

	It("fails when the archive does not contain the binary", func() {
		assets["/"+repo+"/releases/download/v1.2.3/tool-v1.2.3.tar.gz"] = tarGz("other", payload)
		request.Version = "1.2.3"

		_, err := Install(context.Background(), request)

		Expect(err).To(MatchError(ContainSubstring(`"tool" not found in tool-v1.2.3.tar.gz`)))
	})
})
