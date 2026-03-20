package version

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EvaluateVerifyExpr", func() {
	jdk8Expr := `installed == expected || installed.startsWith("1.8.0") || installed.startsWith("8.0.")`

	It("should accept 1.8.0 for jdk8", func() {
		ok, err := EvaluateVerifyExpr(jdk8Expr, "1.8.0", "jdk8u482-b08", "1.8.0", "linux", "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
	})

	It("should accept 8.0.482+8 for jdk8", func() {
		ok, err := EvaluateVerifyExpr(jdk8Expr, "8.0.482+8", "8.0.482+8", "8.0.482+8", "linux", "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
	})

	It("should accept exact match", func() {
		ok, err := EvaluateVerifyExpr(`installed == expected`, "1.35.0", "1.35.0", "1.35.0", "linux", "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
	})

	It("should reject mismatch", func() {
		ok, err := EvaluateVerifyExpr(`installed == expected`, "1.34.0", "1.35.0", "1.34.0", "linux", "amd64")
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeFalse())
	})

	It("should return error for invalid expression", func() {
		_, err := EvaluateVerifyExpr("bad_func()", "1.0.0", "1.0.0", "1.0.0", "linux", "amd64")
		Expect(err).To(HaveOccurred())
	})

	It("should return error for empty expression", func() {
		_, err := EvaluateVerifyExpr("", "1.0.0", "1.0.0", "1.0.0", "linux", "amd64")
		Expect(err).To(HaveOccurred())
	})
})
