package start

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("instance identity", func() {
	It("keeps the legacy resource name by default and isolates explicit instances", func() {
		Expect(containerName("postgres", "")).To(Equal("deps-start-postgres"))
		Expect(containerName("postgres", "a1b2c3d4")).To(Equal("deps-start-postgres-a1b2c3d4"))
	})

	It("rejects invalid instance identifiers", func() {
		_, err := ResolveOptions([]Option{WithInstanceID("../another-service")})
		Expect(err).To(HaveOccurred())
	})
})
