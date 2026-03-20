package version

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EvaluateVersionFallback", func() {
	kubeExpr := `os == "linux" ? version : version.split(".")[0] + "." + version.split(".")[1] + ".0"`

	It("should return minor-only version on darwin", func() {
		ver, tag, err := EvaluateVersionFallback(kubeExpr, "1.35.3", "v1.35.3", "darwin", "arm64")
		Expect(err).ToNot(HaveOccurred())
		Expect(ver).To(Equal("1.35.0"))
		Expect(tag).To(Equal("v1.35.0"))
	})

	It("should return original version on linux", func() {
		ver, tag, err := EvaluateVersionFallback(kubeExpr, "1.35.3", "v1.35.3", "linux", "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(ver).To(Equal("1.35.3"))
		Expect(tag).To(Equal("v1.35.3"))
	})

	It("should return minor-only version on windows", func() {
		ver, tag, err := EvaluateVersionFallback(kubeExpr, "1.35.3", "v1.35.3", "windows", "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(ver).To(Equal("1.35.0"))
		Expect(tag).To(Equal("v1.35.0"))
	})

	It("should return original version for empty expression", func() {
		ver, tag, err := EvaluateVersionFallback("", "1.35.3", "v1.35.3", "darwin", "arm64")
		Expect(err).ToNot(HaveOccurred())
		Expect(ver).To(Equal("1.35.3"))
		Expect(tag).To(Equal("v1.35.3"))
	})

	It("should return error for invalid expression", func() {
		_, _, err := EvaluateVersionFallback("invalid_func()", "1.35.3", "v1.35.3", "darwin", "arm64")
		Expect(err).To(HaveOccurred())
	})
})
