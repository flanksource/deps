package start

import (
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/start/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Test service isolation", func() {
	DescribeTable("rejects invalid named port overrides", func(ports map[string]int, primary int) {
		_, spec, ok := config.GetService("silo")
		Expect(ok).To(BeTrue())
		Expect(validateServiceOptions(*spec, Options{Ports: ports, Port: primary})).To(HaveOccurred())
	}, Entry("unknown name", map[string]int{"unknown": 19000}, 0), Entry("zero", map[string]int{"s3": 0}, 0), Entry("out of range", map[string]int{"s3": 65536}, 0), Entry("conflicting primary", map[string]int{"s3": 19000}, 19001))
	It("includes secondary ports when comparing persisted start options", func() {
		a := &state.StartOptions{Ports: map[string]int{"s3": 19000, "console": 19001}}
		b := &state.StartOptions{Ports: map[string]int{"s3": 19000, "console": 19002}}
		Expect(a.Equal(b)).To(BeFalse())
	})
	It("maps every Silo port without changing the catalog", func() {
		_, spec, ok := config.GetService("silo")
		Expect(ok).To(BeTrue())
		opts, err := ResolveOptions([]Option{WithPorts(map[string]int{"s3": 19000, "console": 19001})})
		Expect(err).NotTo(HaveOccurred())
		svc := testContext(*spec, opts)
		Expect(configuredPorts(svc)).To(Equal(map[string]int{"s3": 19000, "console": 19001}))
		conn, err := BuildConnection(svc, &state.State{}, RuntimeBinary)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn.URL).To(Equal("http://localhost:19000"))
		Expect(conn.Properties["console_url"]).To(Equal("http://localhost:19001"))
		Expect(spec.Ports[0].Port).To(Equal(9000))
	})
	DescribeTable("provides TLS-capable container services", func(name string) {
		_, spec, ok := config.GetService(name)
		Expect(ok).To(BeTrue())
		Expect(spec.Docker).NotTo(BeNil())
		Expect(spec.Parameters).To(HaveKey("tls"))
		Expect(spec.Parameters).To(HaveKey("tls-dir"))
		Expect(spec.Parameters).To(HaveKey("tls-auth-clients"))
	}, Entry("MongoDB", "mongodb"), Entry("Redis", "redis"), Entry("Valkey", "valkey"))
})
