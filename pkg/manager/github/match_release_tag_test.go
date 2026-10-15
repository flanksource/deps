package github

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("matchReleaseTag", func() {
	// Same shape as the clickhouse registry entry: v26.2.19.43-stable -> 26.2.19+43.
	const fourPartExpr = `tag.contains("-stable") ? tag.substring(1).split("-")[0].split(".")[0] + "." + tag.substring(1).split("-")[0].split(".")[1] + "." + tag.substring(1).split("-")[0].split(".")[2] + "+" + tag.substring(1).split("-")[0].split(".")[3] : ""`
	releases := []restRelease{{TagName: "v26.9.19.9-stable"}, {TagName: "v26.2.19.43-stable"}, {TagName: "v1.4.0"}}

	DescribeTable("finds the release tag for a resolved version",
		func(target, expr, expected string) {
			Expect(matchReleaseTag(releases, target, expr, nil)).To(Equal(expected))
		},
		Entry("exact tag", "v1.4.0", "", "v1.4.0"),
		Entry("tag without v prefix", "1.4.0", "", "v1.4.0"),
		Entry("version_expr transformation", "26.2.19+43", fourPartExpr, "v26.2.19.43-stable"),
		Entry("absent version on this page", "26.1.3+1", fourPartExpr, ""),
	)
})
