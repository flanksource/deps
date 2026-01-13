package version

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Semver Parsing", func() {
	tests := []struct {
		version string
		invalid bool
	}{
		{"1.0.0", false},
		{"2.1.3", false},
		{"v1.0.0", false},
		{"v2.1.3", false},
		{"1.0.0-alpha", false},
		{"1.0.0+build.1", false},
		{"1.0.0-alpha+build.1", false},
		{"1.0", false},
		{"v1.0", false},
		{"jdk17u-2024-02-07-14-14-beta", true},
	}
	It("should parse valid semver versions", func() {
		for _, tt := range tests {

			Expect(IsValidSemanticVersion(tt.version)).To(Equal(!tt.invalid), "version: %s", tt.version)
		}
	})

})

var _ = Describe("IsPrerelease", func() {
	DescribeTable("should detect prereleases",
		func(version string, expected bool) {
			Expect(IsPrerelease(version)).To(Equal(expected), "version: %s", version)
		},
		// Standard semver prereleases
		Entry("standard alpha", "1.0.0-alpha", true),
		Entry("standard beta", "1.0.0-beta", true),
		Entry("standard rc", "1.0.0-rc.1", true),
		Entry("stable version", "1.0.0", false),
		Entry("version with build metadata", "1.0.0+build.1", false),

		// OpenJDK style versions (+ is build number, not semver metadata)
		Entry("openjdk ea-beta", "17.0.18+1-ea-beta", true),
		Entry("openjdk stable", "17.0.17+10", false),
		Entry("openjdk tag ea-beta", "jdk-17.0.18+1-ea-beta", true),
		Entry("openjdk tag stable", "jdk-17.0.17+10", false),

		// Other prerelease patterns
		Entry("dev version", "1.0.0-dev", true),
		Entry("snapshot version", "1.0.0-SNAPSHOT", true),
		Entry("pre version", "1.0.0-pre", true),
	)
})
