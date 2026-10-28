package version

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IsCommitSHA", func() {
	fullSHA := "712ce23a27c49bcf4bf161044c8c2203abecadeb"

	DescribeTable("only accepts a full 40-character hex commit",
		func(input string, expected bool) {
			Expect(IsCommitSHA(input)).To(Equal(expected))
		},
		Entry("full lowercase sha", fullSHA, true),
		Entry("full uppercase sha", strings.ToUpper(fullSHA), true),
		Entry("short sha is ambiguous", fullSHA[:7], false),
		Entry("41 characters", fullSHA+"a", false),
		Entry("non-hex character", "g"+fullSHA[1:], false),
		Entry("semver tag", "v1.8.0", false),
		Entry("alias", "latest", false),
		Entry("empty", "", false),
	)
})
