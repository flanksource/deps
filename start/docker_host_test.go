package start

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("docker endpoint host", func() {
	DescribeTable("resolves the host published ports are reachable on",
		func(endpoint, want string) {
			got, err := dockerEndpointHost(endpoint)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("remote tcp context", "tcp://moshe-home:2375", "moshe-home"),
		Entry("remote tcp with an ip", "tcp://10.0.0.7:2376", "10.0.0.7"),
		Entry("https endpoint", "https://docker.example.test:2376", "docker.example.test"),
		Entry("ssh endpoint with user and port", "ssh://deploy@build-host:2222", "build-host"),
		Entry("ssh endpoint without user", "ssh://build-host", "build-host"),
		Entry("local unix socket", "unix:///var/run/docker.sock", "localhost"),
		Entry("windows named pipe", "npipe:////./pipe/docker_engine", "localhost"),
	)

	DescribeTable("rejects endpoints it cannot derive a host from",
		func(endpoint string) {
			_, err := dockerEndpointHost(endpoint)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(endpoint))
		},
		Entry("unknown scheme", "fd://socket"),
		Entry("ssh without a host", "ssh://"),
		Entry("not a url", "::not a url"),
	)
})
