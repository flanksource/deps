package manager_test

import (
	"testing"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAssetSymlinks(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Asset Symlinks Suite")
}

var _ = Describe("ResolveSymlinkPatterns", func() {
	Context("with exact platform match", func() {
		It("should return exact match patterns", func() {
			patterns := map[string][]string{
				"darwin-arm64": {"bin/*", "lib/tool1"},
				"linux-amd64":  {"bin/linux-*"},
				"*":            {"bin/default"},
			}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			result, err := manager.ResolveSymlinkPatterns(patterns, plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal([]string{"bin/*", "lib/tool1"}))
		})
	})

	Context("with glob pattern match", func() {
		It("should match darwin-* pattern", func() {
			patterns := map[string][]string{
				"darwin-*":    {"bin/darwin-*"},
				"linux-*":     {"bin/linux-*"},
				"*":           {"bin/default"},
			}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			result, err := manager.ResolveSymlinkPatterns(patterns, plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal([]string{"bin/darwin-*"}))
		})

		It("should match linux-* pattern", func() {
			patterns := map[string][]string{
				"darwin-*":    {"bin/darwin-*"},
				"linux-*":     {"bin/linux-*"},
			}
			plat := platform.Platform{OS: "linux", Arch: "amd64"}

			result, err := manager.ResolveSymlinkPatterns(patterns, plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal([]string{"bin/linux-*"}))
		})
	})

	Context("with wildcard pattern", func() {
		It("should fall back to * wildcard", func() {
			patterns := map[string][]string{
				"darwin-amd64": {"bin/darwin"},
				"*":            {"bin/default"},
			}
			plat := platform.Platform{OS: "linux", Arch: "amd64"}

			result, err := manager.ResolveSymlinkPatterns(patterns, plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal([]string{"bin/default"}))
		})
	})

	Context("with no matching patterns", func() {
		It("should return nil without error", func() {
			patterns := map[string][]string{
				"darwin-amd64": {"bin/darwin"},
				"linux-amd64":  {"bin/linux"},
			}
			plat := platform.Platform{OS: "windows", Arch: "amd64"}

			result, err := manager.ResolveSymlinkPatterns(patterns, plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeNil())
		})
	})

	Context("with empty patterns map", func() {
		It("should return nil without error", func() {
			patterns := map[string][]string{}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			result, err := manager.ResolveSymlinkPatterns(patterns, plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeNil())
		})
	})

	Context("with comma-separated patterns", func() {
		It("should match comma-separated platforms", func() {
			patterns := map[string][]string{
				"darwin-*,linux-*": {"bin/unix-tools"},
				"windows-*":        {"bin/windows-tools"},
			}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			result, err := manager.ResolveSymlinkPatterns(patterns, plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal([]string{"bin/unix-tools"}))
		})
	})

	Context("priority order", func() {
		It("should prioritize exact match over glob patterns", func() {
			patterns := map[string][]string{
				"darwin-arm64": {"bin/exact"},
				"darwin-*":     {"bin/glob"},
				"*":            {"bin/wildcard"},
			}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			result, err := manager.ResolveSymlinkPatterns(patterns, plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal([]string{"bin/exact"}))
		})

		It("should prioritize glob patterns over wildcard", func() {
			patterns := map[string][]string{
				"darwin-*": {"bin/glob"},
				"*":        {"bin/wildcard"},
			}
			plat := platform.Platform{OS: "darwin", Arch: "arm64"}

			result, err := manager.ResolveSymlinkPatterns(patterns, plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal([]string{"bin/glob"}))
		})
	})
})
