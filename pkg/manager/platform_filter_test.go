package manager

import (
	"github.com/flanksource/deps/pkg/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ParsePlatformEntry", func() {
	It("should parse entry with platform prefix", func() {
		pattern, value, negated, hasPlatform := ParsePlatformEntry("windows*: bin/tool.exe")
		Expect(pattern).To(Equal("windows*"))
		Expect(value).To(Equal("bin/tool.exe"))
		Expect(negated).To(BeFalse())
		Expect(hasPlatform).To(BeTrue())
	})

	It("should parse entry with negated platform prefix", func() {
		pattern, value, negated, hasPlatform := ParsePlatformEntry("!darwin-*: bin/tool")
		Expect(pattern).To(Equal("darwin-*"))
		Expect(value).To(Equal("bin/tool"))
		Expect(negated).To(BeTrue())
		Expect(hasPlatform).To(BeTrue())
	})

	It("should parse entry without platform prefix", func() {
		pattern, value, negated, hasPlatform := ParsePlatformEntry("bin/default-tool")
		Expect(pattern).To(Equal(""))
		Expect(value).To(Equal("bin/default-tool"))
		Expect(negated).To(BeFalse())
		Expect(hasPlatform).To(BeFalse())
	})

	It("should handle extra whitespace", func() {
		pattern, value, negated, hasPlatform := ParsePlatformEntry("  darwin-*  :  bin/tool  ")
		Expect(pattern).To(Equal("darwin-*"))
		Expect(value).To(Equal("bin/tool"))
		Expect(negated).To(BeFalse())
		Expect(hasPlatform).To(BeTrue())
	})

	It("should handle negation with whitespace", func() {
		pattern, value, negated, hasPlatform := ParsePlatformEntry("  !windows*  :  bin/tool.exe  ")
		Expect(pattern).To(Equal("windows*"))
		Expect(value).To(Equal("bin/tool.exe"))
		Expect(negated).To(BeTrue())
		Expect(hasPlatform).To(BeTrue())
	})
})

var _ = Describe("MatchesPlatform", func() {
	Context("without negation", func() {
		It("should match exact platform", func() {
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}
			Expect(MatchesPlatform("darwin-arm64", plat, false)).To(BeTrue())
		})

		It("should match wildcard pattern", func() {
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}
			Expect(MatchesPlatform("darwin-*", plat, false)).To(BeTrue())
		})

		It("should match OS wildcard pattern", func() {
			plat := platform.Platform{OS: "windows", Arch: "amd64"}
			Expect(MatchesPlatform("windows*", plat, false)).To(BeTrue())
		})

		It("should not match different platform", func() {
			plat := platform.Platform{OS: "linux", Arch: "amd64"}
			Expect(MatchesPlatform("darwin-*", plat, false)).To(BeFalse())
		})
	})

	Context("with negation", func() {
		It("should match when platform does not match pattern", func() {
			plat := platform.Platform{OS: "linux", Arch: "amd64"}
			Expect(MatchesPlatform("darwin-*", plat, true)).To(BeTrue())
		})

		It("should not match when platform matches pattern", func() {
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}
			Expect(MatchesPlatform("darwin-*", plat, true)).To(BeFalse())
		})

		It("should exclude Windows platform", func() {
			plat := platform.Platform{OS: "windows", Arch: "amd64"}
			Expect(MatchesPlatform("windows*", plat, true)).To(BeFalse())
		})

		It("should include non-Windows platforms", func() {
			darwinPlat := platform.Platform{OS: "darwin", Arch: "arm64"}
			linuxPlat := platform.Platform{OS: "linux", Arch: "amd64"}
			Expect(MatchesPlatform("windows*", darwinPlat, true)).To(BeTrue())
			Expect(MatchesPlatform("windows*", linuxPlat, true)).To(BeTrue())
		})
	})
})

var _ = Describe("FilterEntriesByPlatform", func() {
	Context("for Windows platform", func() {
		plat := platform.Platform{OS: "windows", Arch: "amd64"}

		It("should include Windows-specific entries", func() {
			entries := []string{
				"windows*: bin/activemq.bat",
				"!windows*: bin/activemq",
			}
			result := FilterEntriesByPlatform(entries, plat)
			Expect(result).To(Equal([]string{"bin/activemq.bat"}))
		})

		It("should exclude negated Windows entries", func() {
			entries := []string{
				"!windows*: bin/tool.sh",
				"bin/default",
			}
			result := FilterEntriesByPlatform(entries, plat)
			Expect(result).To(Equal([]string{"bin/default"}))
		})

		It("should include platform-agnostic entries", func() {
			entries := []string{
				"bin/tool",
				"lib/library",
			}
			result := FilterEntriesByPlatform(entries, plat)
			Expect(result).To(Equal([]string{"bin/tool", "lib/library"}))
		})
	})

	Context("for macOS platform", func() {
		plat := platform.Platform{OS: "darwin", Arch: "arm64"}

		It("should include darwin-specific entries", func() {
			entries := []string{
				"darwin-*: Contents/Home/bin/*",
				"linux-*: jdk-17/bin/*",
			}
			result := FilterEntriesByPlatform(entries, plat)
			Expect(result).To(Equal([]string{"Contents/Home/bin/*"}))
		})

		It("should include negated Windows entries on macOS", func() {
			entries := []string{
				"windows*: bin/tool.bat",
				"!windows*: bin/tool.sh",
			}
			result := FilterEntriesByPlatform(entries, plat)
			Expect(result).To(Equal([]string{"bin/tool.sh"}))
		})
	})

	Context("for Linux platform", func() {
		plat := platform.Platform{OS: "linux", Arch: "amd64"}

		It("should include linux-specific entries", func() {
			entries := []string{
				"darwin-*: Contents/Home/bin/*",
				"linux-*: jdk-17/bin/*",
			}
			result := FilterEntriesByPlatform(entries, plat)
			Expect(result).To(Equal([]string{"jdk-17/bin/*"}))
		})

		It("should filter multiple patterns correctly", func() {
			entries := []string{
				"windows*: bin/win-tool.exe",
				"darwin-*: bin/mac-tool",
				"linux-*: bin/linux-tool",
				"bin/universal-tool",
			}
			result := FilterEntriesByPlatform(entries, plat)
			Expect(result).To(Equal([]string{"bin/linux-tool", "bin/universal-tool"}))
		})
	})

	Context("with post_process examples", func() {
		It("should filter post_process commands for Windows", func() {
			plat := platform.Platform{OS: "windows", Arch: "amd64"}
			entries := []string{
				`!windows*: rm(glob("*.bat"))`,
				`chmod("binary", "0755")`,
			}
			result := FilterEntriesByPlatform(entries, plat)
			Expect(result).To(Equal([]string{`chmod("binary", "0755")`}))
		})

		It("should filter post_process commands for Linux", func() {
			plat := platform.Platform{OS: "linux", Arch: "amd64"}
			entries := []string{
				`!windows*: rm(glob("*.bat"))`,
				`chmod("binary", "0755")`,
			}
			result := FilterEntriesByPlatform(entries, plat)
			Expect(result).To(Equal([]string{`rm(glob("*.bat"))`, `chmod("binary", "0755")`}))
		})
	})

	Context("with empty or nil input", func() {
		plat := platform.Platform{OS: "darwin", Arch: "arm64"}

		It("should return empty slice for nil entries", func() {
			result := FilterEntriesByPlatform(nil, plat)
			Expect(result).To(BeNil())
		})

		It("should return empty slice for empty entries", func() {
			result := FilterEntriesByPlatform([]string{}, plat)
			Expect(result).To(HaveLen(0))
		})
	})
})
