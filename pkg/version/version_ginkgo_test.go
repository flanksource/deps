package version

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Version", func() {
	Describe("Normalize", func() {
		DescribeTable("should normalize various version formats",
			func(input, expected string) {
				result := Normalize(input)
				Expect(result).To(Equal(expected))
			},
			Entry("with v prefix", "v1.2.3", "1.2.3"),
			Entry("with V prefix", "V1.2.3", "1.2.3"),
			Entry("without prefix", "1.2.3", "1.2.3"),
			Entry("with release- prefix", "release-1.2.3", "1.2.3"),
			Entry("with Release- prefix", "Release-1.2.3", "1.2.3"),
			Entry("with version- prefix", "version-1.2.3", "1.2.3"),
			Entry("with Version- prefix", "Version-1.2.3", "1.2.3"),
			Entry("with -release suffix", "1.2.3-release", "1.2.3"),
			Entry("with -Release suffix", "1.2.3-Release", "1.2.3"),
			Entry("with alpha suffix", "v1.2.3-alpha", "1.2.3-alpha"),
			Entry("empty string", "", ""),
			Entry("with whitespace", " v1.2.3 ", "1.2.3"),
		)
	})

	Describe("ExtractFromOutput", func() {
		Context("when extracting version from output", func() {
			It("should extract version from kubectl version output", func() {
				result, err := ExtractFromOutput("kubectl version v1.28.2", "")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("1.28.2"))
			})

			It("should extract version from version: output", func() {
				result, err := ExtractFromOutput("version: 3.5.0", "")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("3.5.0"))
			})

			It("should extract version from jq output", func() {
				result, err := ExtractFromOutput("jq-1.6", "")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("1.6"))
			})

			It("should extract version with custom pattern", func() {
				result, err := ExtractFromOutput("Client Version: v1.28.2", `Client Version:\s*v?(\d+\.\d+\.\d+)`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("1.28.2"))
			})
		})

		Context("when extraction fails", func() {
			It("should return error when no version found", func() {
				result, err := ExtractFromOutput("no version found", "")
				Expect(err).To(HaveOccurred())
				Expect(result).To(Equal(""))
			})

			It("should return error with invalid regex", func() {
				result, err := ExtractFromOutput("version 1.2.3", `invalid[regex`)
				Expect(err).To(HaveOccurred())
				Expect(result).To(Equal(""))
			})
		})
	})

	Describe("Compare", func() {
		DescribeTable("should compare versions correctly",
			func(v1, v2 string, expected int) {
				result, err := Compare(v1, v2)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(expected))
			},
			Entry("same version different prefixes", "1.2.3", "v1.2.3", 0),
			Entry("same version different prefixes reverse", "v3.5.0", "3.5.0", 0),
			Entry("v1 less than v2", "1.2.3", "1.2.4", -1),
			Entry("v1 greater than v2", "1.3.0", "1.2.9", 1),
			Entry("major version greater", "2.0.0", "1.9.9", 1),
			Entry("alpha less than beta", "1.0.0-alpha", "1.0.0-beta", -1),
		)
	})

	Describe("IsCompatible", func() {
		DescribeTable("should check version compatibility",
			func(installed, required string, expected bool) {
				result, err := IsCompatible(installed, required)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(expected))
			},
			Entry("installed greater than required", "v3.5.0", "3.4.0", true),
			Entry("same version different prefixes", "3.5.0", "v3.5.0", true),
			Entry("installed less than required", "3.4.0", "3.5.0", false),
			Entry("different major version higher", "4.0.0", "3.5.0", false),
			Entry("different major version lower", "2.9.0", "3.0.0", false),
		)
	})

	Describe("SatisfiesConstraint", func() {
		DescribeTable("should check constraint satisfaction",
			func(version, constraint string, expected bool) {
				result, err := SatisfiesConstraint(version, constraint)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(expected))
			},
			Entry("latest constraint", "v1.2.3", "latest", true),
			Entry("exact version match", "1.2.3", "1.2.3", true),
			Entry("exact version with prefix", "v1.2.3", "1.2.3", true),
			Entry("caret constraint satisfied", "1.2.4", "^1.2.0", true),
			Entry("tilde constraint not satisfied", "1.3.0", "~1.2.0", false),
			Entry("caret constraint not satisfied", "2.0.0", "^1.2.0", false),
			Entry("greater than constraint", "1.2.3", ">=1.2.0", true),
		)
	})
})