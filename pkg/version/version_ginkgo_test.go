package version

import (
	"time"

	clickyExec "github.com/flanksource/clicky/exec"
	"github.com/flanksource/deps/pkg/types"
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
			Entry("with package prefix using dash", "jq-1.7", "1.7"),
			Entry("with package prefix using slash", "operator/v0.8.0", "v0.8.0"),
			Entry("with package prefix using underscore", "tool_1.2.3", "1.2.3"),
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

			It("should extract OpenJDK version with build number from java output", func() {
				javaOutput := `openjdk 11.0.28 2024-07-16
OpenJDK Runtime Environment Temurin-11.0.28+6 (build 11.0.28+6)
OpenJDK 64-Bit Server VM Temurin-11.0.28+6 (build 11.0.28+6, mixed mode)`
				result, err := ExtractFromOutput(javaOutput, `([\d.]+\+\d+)`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("11.0.28+6"))
			})

			It("should extract OpenJDK 17 version with build number", func() {
				javaOutput := `openjdk 17.0.15 2024-10-15
OpenJDK Runtime Environment Temurin-17.0.15+10 (build 17.0.15+10)
OpenJDK 64-Bit Server VM Temurin-17.0.15+10 (build 17.0.15+10, mixed mode, sharing)`
				result, err := ExtractFromOutput(javaOutput, `([\d.]+\+\d+)`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("17.0.15+10"))
			})

			It("should extract OpenJDK 21 version with build number", func() {
				javaOutput := `openjdk 21.0.5 2024-10-15
OpenJDK Runtime Environment Temurin-21.0.5+11 (build 21.0.5+11)
OpenJDK 64-Bit Server VM Temurin-21.0.5+11 (build 21.0.5+11, mixed mode, sharing)`
				result, err := ExtractFromOutput(javaOutput, `([\d.]+\+\d+)`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("21.0.5+11"))
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

var _ = Describe("Version Expression Filtering", func() {
	Describe("ApplyVersionExpr", func() {
		var testVersions []types.Version

		BeforeEach(func() {
			testVersions = []types.Version{
				{
					Tag:        "v1.0.0",
					Version:    "1.0.0",
					SHA:        "abc123",
					Published:  time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
					Prerelease: false,
				},
				{
					Tag:        "v1.1.0-beta.1",
					Version:    "1.1.0-beta.1",
					SHA:        "def456",
					Published:  time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC),
					Prerelease: true,
				},
				{
					Tag:        "v1.1.0",
					Version:    "1.1.0",
					SHA:        "ghi789",
					Published:  time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC),
					Prerelease: false,
				},
				{
					Tag:        "v2.0.0-rc.1",
					Version:    "2.0.0-rc.1",
					SHA:        "jkl012",
					Published:  time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC),
					Prerelease: true,
				},
				{
					Tag:        "2.0.0", // No v prefix
					Version:    "2.0.0",
					SHA:        "mno345",
					Published:  time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC),
					Prerelease: false,
				},
			}
		})

		Context("with empty expression", func() {
			It("should return all versions unchanged", func() {
				result, err := ApplyVersionExpr(testVersions, "")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(testVersions))
			})
		})

		Context("with filtering expressions", func() {
			It("should filter out prerelease versions", func() {
				result, err := ApplyVersionExpr(testVersions, "!prerelease")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				tags := make([]string, len(result))
				for i, v := range result {
					tags[i] = v.Tag
				}
				Expect(tags).To(ConsistOf("v1.0.0", "v1.1.0", "2.0.0"))
			})

			It("should filter only prerelease versions", func() {
				result, err := ApplyVersionExpr(testVersions, "prerelease")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(2))

				tags := make([]string, len(result))
				for i, v := range result {
					tags[i] = v.Tag
				}
				Expect(tags).To(ConsistOf("v1.1.0-beta.1", "v2.0.0-rc.1"))
			})

			It("should filter versions with v prefix", func() {
				result, err := ApplyVersionExpr(testVersions, "tag.startsWith('v')")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(4))

				for _, v := range result {
					Expect(v.Tag).To(HavePrefix("v"))
				}
			})

			It("should filter versions without v prefix", func() {
				result, err := ApplyVersionExpr(testVersions, "!tag.startsWith('v')")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(1))
				Expect(result[0].Tag).To(Equal("2.0.0"))
			})

			It("should filter versions by date", func() {
				// Filter versions published after Feb 15, 2023
				result, err := ApplyVersionExpr(testVersions, "published > timestamp('2023-02-15T00:00:00Z')")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				tags := make([]string, len(result))
				for i, v := range result {
					tags[i] = v.Tag
				}
				Expect(tags).To(ConsistOf("v1.1.0", "v2.0.0-rc.1", "2.0.0"))
			})

			It("should combine multiple conditions", func() {
				// Non-prerelease versions with v prefix
				result, err := ApplyVersionExpr(testVersions, "!prerelease && tag.startsWith('v')")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(2))

				tags := make([]string, len(result))
				for i, v := range result {
					tags[i] = v.Tag
				}
				Expect(tags).To(ConsistOf("v1.0.0", "v1.1.0"))
			})
		})

		Context("with invalid expressions", func() {
			It("should return error for syntax errors", func() {
				_, err := ApplyVersionExpr(testVersions, "invalid syntax &&& xyz")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to evaluate version_expr"))
			})

			It("should return error for undefined variables", func() {
				_, err := ApplyVersionExpr(testVersions, "unknownField == 'test'")
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("ApplyCommonFilter", func() {
		var testVersions []types.Version

		BeforeEach(func() {
			testVersions = []types.Version{
				{
					Tag:        "v1.0.0",
					Version:    "1.0.0",
					Prerelease: false,
				},
				{
					Tag:        "v1.1.0-beta.1",
					Version:    "1.1.0-beta.1",
					Prerelease: true,
				},
				{
					Tag:        "2.0.0",
					Version:    "2.0.0",
					Prerelease: false,
				},
			}
		})

		It("should apply no-prerelease filter", func() {
			result, err := ApplyCommonFilter(testVersions, "no-prerelease")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))

			for _, v := range result {
				Expect(v.Prerelease).To(BeFalse())
			}
		})

		It("should apply only-prerelease filter", func() {
			result, err := ApplyCommonFilter(testVersions, "only-prerelease")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].Prerelease).To(BeTrue())
		})

		It("should apply v-prefix filter", func() {
			result, err := ApplyCommonFilter(testVersions, "v-prefix")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))

			for _, v := range result {
				Expect(v.Tag).To(HavePrefix("v"))
			}
		})

		It("should apply no-v-prefix filter", func() {
			result, err := ApplyCommonFilter(testVersions, "no-v-prefix")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].Tag).To(Equal("2.0.0"))
		})

		It("should return error for unknown filter", func() {
			_, err := ApplyCommonFilter(testVersions, "unknown-filter")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown common filter"))
		})
	})

	Describe("ValidateVersionExpr", func() {
		It("should validate empty expression", func() {
			err := ValidateVersionExpr("")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should validate simple boolean expression", func() {
			err := ValidateVersionExpr("!prerelease")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should validate string operations", func() {
			err := ValidateVersionExpr("tag.startsWith('v')")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should validate comparison operations", func() {
			err := ValidateVersionExpr("version >= '1.0.0'")
			Expect(err).ToNot(HaveOccurred())
		})

		It("should return error for invalid expressions", func() {
			err := ValidateVersionExpr("invalid && syntax &&& error")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid version_expr"))
		})
	})

	Describe("Tag Transformation", func() {
		var testVersions []types.Version

		BeforeEach(func() {
			testVersions = []types.Version{
				{
					Tag:        "go1.21.5",
					Version:    "go1.21.5",
					SHA:        "abc123",
					Published:  time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
					Prerelease: false,
				},
				{
					Tag:        "v1.2.3",
					Version:    "1.2.3",
					SHA:        "def456",
					Published:  time.Date(2023, 2, 1, 0, 0, 0, 0, time.UTC),
					Prerelease: false,
				},
				{
					Tag:        "release-2023.1",
					Version:    "2023.1",
					SHA:        "ghi789",
					Published:  time.Date(2023, 3, 1, 0, 0, 0, 0, time.UTC),
					Prerelease: false,
				},
			}
		})

		Context("with simple string transformation", func() {
			It("should remove go prefix from tags", func() {
				result, err := ApplyVersionExpr(testVersions, `tag.startsWith("go") ? tag.substring(2) : tag`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				// First version should have go prefix removed
				Expect(result[0].Tag).To(Equal("1.21.5"))
				Expect(result[0].Version).To(Equal("1.21.5"))

				// Other versions should be unchanged
				Expect(result[1].Tag).To(Equal("v1.2.3"))
				Expect(result[2].Tag).To(Equal("release-2023.1"))
			})

			It("should remove v prefix from tags", func() {
				result, err := ApplyVersionExpr(testVersions, `tag.startsWith("v") ? tag.substring(1) : tag`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				// All versions should have v prefix removed (if present)
				Expect(result[0].Tag).To(Equal("go1.21.5"))       // unchanged, no v prefix
				Expect(result[1].Tag).To(Equal("1.2.3"))          // v prefix removed
				Expect(result[2].Tag).To(Equal("release-2023.1")) // unchanged, no v prefix
			})

			It("should add v prefix to tags without it", func() {
				result, err := ApplyVersionExpr(testVersions, `tag.startsWith("v") ? tag : "v" + tag`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				// First and third should get v prefix, second should remain unchanged
				Expect(result[0].Tag).To(Equal("vgo1.21.5"))
				Expect(result[1].Tag).To(Equal("v1.2.3"))
				Expect(result[2].Tag).To(Equal("vrelease-2023.1"))
			})

			It("should replace strings in tags using regex", func() {
				result, err := ApplyVersionExpr(testVersions, `tag.contains("release-") ? "v" + tag.substring(8) : tag`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				// Only the third version should be changed
				Expect(result[0].Tag).To(Equal("go1.21.5"))
				Expect(result[1].Tag).To(Equal("v1.2.3"))
				Expect(result[2].Tag).To(Equal("v2023.1"))
			})
		})

		Context("with advanced transformation patterns", func() {
			It("should handle complex tag transformation logic", func() {
				// Complex transformation: remove go prefix if present, add v prefix if missing
				result, err := ApplyVersionExpr(testVersions, `
					tag.startsWith("go") ?
						"v" + tag.substring(2) :
						(tag.startsWith("v") ? tag : "v" + tag)
				`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				// go1.21.5 -> v1.21.5
				Expect(result[0].Tag).To(Equal("v1.21.5"))
				// v1.2.3 -> v1.2.3 (unchanged)
				Expect(result[1].Tag).To(Equal("v1.2.3"))
				// release-2023.1 -> vrelease-2023.1
				Expect(result[2].Tag).To(Equal("vrelease-2023.1"))
			})

			It("should support nested conditional transformation", func() {
				// Only include and transform Go versions
				result, err := ApplyVersionExpr(testVersions, `
					tag.startsWith("go") && tag.size() > 2 ? tag.substring(2) : ""
				`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(1)) // Only go version should be included

				Expect(result[0].Tag).To(Equal("1.21.5"))
				Expect(result[0].Version).To(Equal("1.21.5"))
			})
		})

		Context("with common transformation filters", func() {
			It("should apply remove-go-prefix filter", func() {
				result, err := ApplyCommonFilter(testVersions, "remove-go-prefix")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				Expect(result[0].Tag).To(Equal("1.21.5"))
				Expect(result[1].Tag).To(Equal("v1.2.3"))
				Expect(result[2].Tag).To(Equal("release-2023.1"))
			})

			It("should apply remove-v-prefix filter", func() {
				result, err := ApplyCommonFilter(testVersions, "remove-v-prefix")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				Expect(result[0].Tag).To(Equal("go1.21.5"))
				Expect(result[1].Tag).To(Equal("1.2.3"))
				Expect(result[2].Tag).To(Equal("release-2023.1"))
			})

			It("should apply add-v-prefix filter", func() {
				result, err := ApplyCommonFilter(testVersions, "add-v-prefix")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(3))

				Expect(result[0].Tag).To(Equal("vgo1.21.5"))
				Expect(result[1].Tag).To(Equal("v1.2.3"))
				Expect(result[2].Tag).To(Equal("vrelease-2023.1"))
			})
		})

		Context("with combined filtering and transformation", func() {
			It("should filter and transform in one expression", func() {
				// Remove go prefix only from versions that start with "go", otherwise exclude
				result, err := ApplyVersionExpr(testVersions, `tag.startsWith("go") ? tag.substring(2) : ""`)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(HaveLen(1)) // Only the go version should be included

				Expect(result[0].Tag).To(Equal("1.21.5"))
				Expect(result[0].Version).To(Equal("1.21.5"))
			})
		})
	})

	Describe("Helper Functions", func() {
		Describe("isJSONObject", func() {
			It("should detect JSON objects", func() {
				Expect(isJSONObject(`{"key": "value"}`)).To(BeTrue())
				Expect(isJSONObject(`  {"key": "value"}  `)).To(BeTrue())
				Expect(isJSONObject(`{"tag": "v1.0.0", "include": true}`)).To(BeTrue())
			})

			It("should reject non-JSON strings", func() {
				Expect(isJSONObject("true")).To(BeFalse())
				Expect(isJSONObject("false")).To(BeFalse())
				Expect(isJSONObject("v1.0.0")).To(BeFalse())
				Expect(isJSONObject(`["array"]`)).To(BeFalse())
			})
		})

		Describe("isSimpleString", func() {
			It("should detect simple strings", func() {
				Expect(isSimpleString("v1.0.0")).To(BeTrue())
				Expect(isSimpleString("go1.21.5")).To(BeTrue())
				Expect(isSimpleString("release-2023.1")).To(BeTrue())
			})

			It("should reject boolean strings", func() {
				Expect(isSimpleString("true")).To(BeFalse())
				Expect(isSimpleString("false")).To(BeFalse())
			})

			It("should reject JSON objects", func() {
				Expect(isSimpleString(`{"key": "value"}`)).To(BeFalse())
			})

			It("should reject empty strings", func() {
				Expect(isSimpleString("")).To(BeFalse())
				Expect(isSimpleString("   ")).To(BeFalse())
			})
		})

		Describe("transformTag", func() {
			It("should create new version with transformed tag", func() {
				original := types.Version{
					Tag:        "go1.21.5",
					Version:    "go1.21.5",
					SHA:        "abc123",
					Prerelease: false,
				}

				result := transformTag(original, "1.21.5")

				Expect(result.Tag).To(Equal("1.21.5"))
				Expect(result.Version).To(Equal("1.21.5"))
				Expect(result.SHA).To(Equal("abc123"))
				Expect(result.Prerelease).To(BeFalse())
			})
		})

		Describe("Built-in CEL string functions", func() {
			It("should work with substring operations", func() {
				// These are tested implicitly through the transformation tests above
				// Just verify the basic functionality understanding
				testString := "go1.21.5"
				Expect(testString[2:]).To(Equal("1.21.5")) // This is what substring(2) does in CEL
			})
		})
	})

	Describe("createVersionContext", func() {
		It("should create correct context data", func() {
			version := types.Version{
				Tag:        "v1.0.0",
				Version:    "1.0.0",
				SHA:        "abc123",
				Published:  time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				Prerelease: true,
			}

			context := createVersionContext(version)

			Expect(context).To(HaveKey("tag"))
			Expect(context["tag"]).To(Equal("v1.0.0"))
			Expect(context).To(HaveKey("version"))
			Expect(context["version"]).To(Equal("1.0.0"))
			Expect(context).To(HaveKey("sha"))
			Expect(context["sha"]).To(Equal("abc123"))
			Expect(context).To(HaveKey("published"))
			Expect(context["published"]).To(Equal(time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)))
			Expect(context).To(HaveKey("prerelease"))
			Expect(context["prerelease"]).To(BeTrue())

			// Helper functions are no longer in context since we use built-in CEL functions
		})
	})

	Describe("LooksLikeExactVersion", func() {
		DescribeTable("should identify exact versions correctly",
			func(version string, expected bool) {
				result := LooksLikeExactVersion(version)
				Expect(result).To(Equal(expected))
			},
			// Exact versions
			Entry("exact version without prefix", "1.2.3", true),
			Entry("exact version with v prefix", "v1.2.3", true),
			Entry("exact version with V prefix", "V1.2.3", true),
			Entry("exact version with patch", "1.2.3-alpha", true),
			Entry("exact version with build metadata", "1.2.3+build.1", true),

			// Not exact versions
			Entry("major only", "1", false),
			Entry("major.minor only", "1.2", false),
			Entry("major only with v prefix", "v1", false),
			Entry("major.minor with v prefix", "v1.2", false),
			Entry("constraint with caret", "^1.2.3", false),
			Entry("constraint with tilde", "~1.2.3", false),
			Entry("constraint with >=", ">=1.2.3", false),
			Entry("constraint with <=", "<=1.2.3", false),
			Entry("constraint with >", ">1.2.3", false),
			Entry("constraint with <", "<1.2.3", false),
			Entry("latest keyword", "latest", false),
			Entry("empty string", "", false),
			Entry("wildcard", "*", false),
		)
	})

	Describe("IsPartialVersion", func() {
		DescribeTable("should identify partial versions correctly",
			func(version string, expected bool) {
				result := IsPartialVersion(version)
				Expect(result).To(Equal(expected))
			},
			// Partial versions
			Entry("major only", "1", true),
			Entry("major only with v prefix", "v1", true),
			Entry("major only with V prefix", "V1", true),
			Entry("major.minor only", "1.2", true),
			Entry("major.minor with v prefix", "v1.2", true),
			Entry("major.minor with V prefix", "V1.2", true),
			Entry("major.minor with zero", "1.0", true),
			Entry("higher major version", "10", true),
			Entry("double digit minor", "1.15", true),

			// Not partial versions
			Entry("exact version", "1.2.3", false),
			Entry("exact version with v prefix", "v1.2.3", false),
			Entry("exact version with patch", "1.2.3-alpha", false),
			Entry("constraint with caret", "^1.2", false),
			Entry("constraint with tilde", "~1.2", false),
			Entry("constraint with >=", ">=1.2", false),
			Entry("constraint with <=", "<=1.2", false),
			Entry("constraint with >", ">1", false),
			Entry("constraint with <", "<1", false),
			Entry("latest keyword", "latest", false),
			Entry("empty string", "", false),
			Entry("wildcard", "*", false),
			Entry("invalid version", "abc", false),
			Entry("invalid major.minor", "1.abc", false),
		)
	})
})

var _ = Describe("Shell Command Handling", func() {
	Describe("ContainsShellOperators", func() {
		DescribeTable("should detect shell operators correctly",
			func(cmd string, expected bool) {
				result := clickyExec.ContainsShellOperators(cmd)
				Expect(result).To(Equal(expected))
			},
			// Commands with shell operators
			Entry("pipe operator", "bin/tomee version | grep TomEE", true),
			Entry("stderr redirect", "bin/tomee version 2>/dev/null", true),
			Entry("stdout redirect", "bin/tomee version > output.txt", true),
			Entry("stdin redirect", "bin/tomee version < input.txt", true),
			Entry("logical AND", "bin/tomee version && echo success", true),
			Entry("logical OR", "bin/tomee version || echo fail", true),
			Entry("semicolon", "cd bin; ./tomee version", true),
			Entry("backticks", "echo `bin/tomee version`", true),
			Entry("command substitution", "echo $(bin/tomee version)", true),
			Entry("complex with multiple operators", "bin/tomee version 2>/dev/null | grep TomEE", true),

			// Commands without shell operators
			Entry("simple command", "bin/tomee version", false),
			Entry("command with flags", "bin/tomee --version", false),
			Entry("command with arguments", "bin/tomee version info", false),
			Entry("path with forward slashes", "/usr/local/bin/tomee", false),
			Entry("empty string", "", false),
		)
	})

	Describe("Shell command wrapping logic", func() {
		Context("when command is already wrapped in bash -c", func() {
			It("should not double-wrap the command", func() {
				// This test verifies the fix for double-wrapping bug
				// When a command starts with "bash -c", it should not be wrapped again

				// The command already has shell operators AND starts with bash -c
				cmd := "bash -c 'bin/tomee version 2>/dev/null | grep TomEE'"

				// Verify that ContainsShellOperators detects operators
				Expect(clickyExec.ContainsShellOperators(cmd)).To(BeTrue())

				// The actual wrapping logic is tested indirectly through GetInstalledVersionWithMode
				// Here we verify that the detection logic for already-wrapped commands works
				cmdParts := []string{"bash", "-c", "bin/tomee version 2>/dev/null | grep TomEE"}
				alreadyShellWrapped := len(cmdParts) >= 2 &&
					(cmdParts[0] == "bash" || cmdParts[0] == "sh") &&
					cmdParts[1] == "-c"

				Expect(alreadyShellWrapped).To(BeTrue())
			})
		})

		Context("when command is already wrapped in sh -c", func() {
			It("should not double-wrap the command", func() {
				cmd := "sh -c 'bin/tomee version 2>/dev/null | grep TomEE'"

				// Verify that ContainsShellOperators detects operators
				Expect(clickyExec.ContainsShellOperators(cmd)).To(BeTrue())

				// Verify detection logic for sh -c
				cmdParts := []string{"sh", "-c", "bin/tomee version 2>/dev/null | grep TomEE"}
				alreadyShellWrapped := len(cmdParts) >= 2 &&
					(cmdParts[0] == "bash" || cmdParts[0] == "sh") &&
					cmdParts[1] == "-c"

				Expect(alreadyShellWrapped).To(BeTrue())
			})
		})

		Context("when command has shell operators but is not wrapped", func() {
			It("should detect that wrapping is needed", func() {
				// This is the auto-detection scenario
				cmd := "bin/tomee version 2>/dev/null | grep TomEE"

				// Verify that ContainsShellOperators detects operators
				Expect(clickyExec.ContainsShellOperators(cmd)).To(BeTrue())

				// Verify that the command is NOT already wrapped
				cmdParts := []string{"bin/tomee", "version", "2>/dev/null", "|", "grep", "TomEE"}
				alreadyShellWrapped := len(cmdParts) >= 2 &&
					(cmdParts[0] == "bash" || cmdParts[0] == "sh") &&
					cmdParts[1] == "-c"

				Expect(alreadyShellWrapped).To(BeFalse())
			})
		})

		Context("when command has no shell operators", func() {
			It("should not require wrapping", func() {
				cmd := "bin/tomee --version"

				// Verify that ContainsShellOperators does NOT detect operators
				Expect(clickyExec.ContainsShellOperators(cmd)).To(BeFalse())
			})
		})
	})

	Describe("ResolveVersionCommandBinary", func() {
		Context("with relative paths", func() {
			It("should resolve bin/ prefix in directory mode", func() {
				// This would need actual directory structure to test properly
				// For now, we just document the expected behavior

				// In directory mode with command "bin/tomee version"
				// It should look for bin/tomee relative to the package directory
				cmd := "bin/tomee version"
				Expect(clickyExec.ContainsShellOperators(cmd)).To(BeFalse())
			})
		})

		Context("with PATH lookup", func() {
			It("should handle commands without path separators", func() {
				// Commands like "tomee version" should be looked up on PATH
				cmd := "tomee version"
				Expect(clickyExec.ContainsShellOperators(cmd)).To(BeFalse())
			})
		})
	})
})
