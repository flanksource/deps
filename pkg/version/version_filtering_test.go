package version

import (
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Version Filtering Functions", func() {
	Describe("IsValidSemanticVersion", func() {
		DescribeTable("should correctly identify valid semantic versions",
			func(input string, expected bool) {
				result := IsValidSemanticVersion(input)
				Expect(result).To(Equal(expected))
			},
			Entry("valid semver with v prefix", "v1.2.3", true),
			Entry("valid semver without prefix", "1.2.3", true),
			Entry("valid semver with prerelease", "v2.0.0-alpha.1", true),
			Entry("valid semver with build metadata", "v1.0.0+20230101", true),
			Entry("empty string", "", false),
			Entry("feature branch", "feature-xyz", false),
			Entry("dev branch", "dev-branch", false),
			Entry("nightly build with date", "nightly-20240101", false), // not valid - 20240101 lacks MAJOR.MINOR format
			Entry("random text", "random-text", false),
			Entry("just letters", "abcdef", false),
			Entry("partial version", "v1", true), // This is valid semver as 1.0.0
			Entry("three part version", "1.2.3", true),
			Entry("release prefix", "release-1.2.3", true),
			Entry("branch with letters", "feature-branch-xyz", false),
		)
	})

	Describe("FilterValidVersions", func() {
		It("should filter out non-semantic versions", func() {
			input := []string{
				"v1.2.3",
				"1.0.0",
				"feature-branch",
				"v2.0.0-alpha.1",
				"dev-xyz",
				"nightly-20240101", // Not valid - 20240101 lacks MAJOR.MINOR format
				"3.1.4",
				"random-text",
			}

			result := FilterValidVersions(input)

			Expect(result).To(ConsistOf("v1.2.3", "1.0.0", "v2.0.0-alpha.1", "3.1.4"))
		})

		It("should handle empty input", func() {
			result := FilterValidVersions([]string{})
			Expect(result).To(BeEmpty())
		})

		It("should handle all invalid versions", func() {
			input := []string{"feature-branch", "dev-xyz", "random-text"}
			result := FilterValidVersions(input)
			Expect(result).To(BeEmpty())
		})
	})

	Describe("ProcessTags", func() {
		It("should convert valid tags to Version structs", func() {
			input := []string{
				"v1.2.3",
				"2.0.0-alpha.1",
				"feature-branch", // should be filtered out
				"v3.1.0",
				"dev-xyz", // should be filtered out
			}

			result := ProcessTags(input)

			Expect(result).To(HaveLen(3))

			// Find v1.2.3
			var v123 *types.Version
			for i := range result {
				if result[i].Tag == "v1.2.3" {
					v123 = &result[i]
					break
				}
			}
			Expect(v123).ToNot(BeNil())
			Expect(v123.Version).To(Equal("1.2.3"))
			Expect(v123.Prerelease).To(BeFalse())

			// Find 2.0.0-alpha.1
			var alpha *types.Version
			for i := range result {
				if result[i].Tag == "2.0.0-alpha.1" {
					alpha = &result[i]
					break
				}
			}
			Expect(alpha).ToNot(BeNil())
			Expect(alpha.Version).To(Equal("2.0.0-alpha.1"))
			Expect(alpha.Prerelease).To(BeTrue())
		})

		It("should handle empty input", func() {
			result := ProcessTags([]string{})
			Expect(result).To(BeEmpty())
		})
	})

	Describe("SortVersionStructs", func() {
		It("should sort versions in descending order", func() {
			input := []types.Version{
				{Tag: "v1.0.0", Version: "1.0.0", Prerelease: false},
				{Tag: "v2.1.0", Version: "2.1.0", Prerelease: false},
				{Tag: "v1.5.0", Version: "1.5.0", Prerelease: false},
				{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
			}

			result := SortVersionStructs(input)

			Expect(result).To(HaveLen(4))
			Expect(result[0].Version).To(Equal("2.1.0"))
			Expect(result[1].Version).To(Equal("2.0.0"))
			Expect(result[2].Version).To(Equal("1.5.0"))
			Expect(result[3].Version).To(Equal("1.0.0"))
		})

		It("should handle single version", func() {
			input := []types.Version{
				{Tag: "v1.0.0", Version: "1.0.0", Prerelease: false},
			}

			result := SortVersionStructs(input)
			Expect(result).To(Equal(input))
		})

		It("should handle empty input", func() {
			result := SortVersionStructs([]types.Version{})
			Expect(result).To(BeEmpty())
		})

		It("should not modify original slice", func() {
			input := []types.Version{
				{Tag: "v1.0.0", Version: "1.0.0", Prerelease: false},
				{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
			}
			originalFirst := input[0].Version

			SortVersionStructs(input)

			// Original slice should be unchanged
			Expect(input[0].Version).To(Equal(originalFirst))
		})
	})

	Describe("FilterAndSortVersions", func() {
		It("should filter, sort, and optionally exclude prereleases", func() {
			input := []string{
				"v1.0.0",
				"v2.1.0-alpha.1",
				"feature-branch", // filtered out
				"v1.5.0",
				"v2.0.0",
				"dev-xyz", // filtered out
			}

			// Test with prereleases included
			result := FilterAndSortVersions(input, false)
			Expect(result).To(HaveLen(4))
			Expect(result[0].Version).To(Equal("2.1.0-alpha.1")) // newest
			Expect(result[1].Version).To(Equal("2.0.0"))
			Expect(result[2].Version).To(Equal("1.5.0"))
			Expect(result[3].Version).To(Equal("1.0.0"))

			// Test with only stable versions
			stableResult := FilterAndSortVersions(input, true)
			Expect(stableResult).To(HaveLen(3))
			Expect(stableResult[0].Version).To(Equal("2.0.0")) // newest stable
			Expect(stableResult[1].Version).To(Equal("1.5.0"))
			Expect(stableResult[2].Version).To(Equal("1.0.0"))
		})

		It("should handle all prerelease versions with stableOnly=true", func() {
			input := []string{
				"v1.0.0-alpha.1",
				"v2.0.0-beta.1",
			}

			result := FilterAndSortVersions(input, true)
			Expect(result).To(BeEmpty())
		})
	})
})
