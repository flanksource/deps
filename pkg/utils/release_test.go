package utils

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRelease(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Release versions")
}

var _ = Describe("release timestamps", func() {
	It("normalizes a release tag to its package version", func() {
		Expect(Normalize("RELEASE.2026-10-26T00-00-00Z")).To(Equal("20261026000000.0.0"))
	})
})
