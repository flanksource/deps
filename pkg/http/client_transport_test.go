package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/flanksource/commons/http/middlewares"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHTTPTransport(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HTTP transport integration")
}

var _ = Describe("an instrumented default HTTP transport", func() {
	It("preserves the wrapper and encoded queries when constructing a deps client", func() {
		original := http.DefaultTransport
		DeferCleanup(func() { http.DefaultTransport = original })
		var calls atomic.Int32
		http.DefaultTransport = middlewares.RoundTripperFunc(func(request *http.Request) (*http.Response, error) {
			calls.Add(1)
			return original.RoundTrip(request)
		})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			w.Header().Set("X-Query", request.URL.RawQuery)
			_, _ = io.WriteString(w, "fixture response")
		}))
		DeferCleanup(server.Close)
		const query = "version=8%2B512&label=hello%20world"
		client := GetHttpClient(func(config *clientConfig) { config.logMode = "off" })
		response, err := client.Get(server.URL + "?" + query)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(response.Body.Close)
		data, err := io.ReadAll(response.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect([]any{calls.Load(), response.Header.Get("X-Query"), string(data)}).
			To(Equal([]any{int32(1), query, "fixture response"}))
	})
})
