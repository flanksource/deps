package installer

import (
	"context"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ParseToolSpec", func() {
	const commit = "712ce23a27c49bcf4bf161044c8c2203abecadeb"

	DescribeTable("splits name, #asset hint and @version",
		func(input string, expected ToolSpec) {
			Expect(ParseToolSpec(input)).To(Equal(expected))
		},
		Entry("bare name", "jq", ToolSpec{Name: "jq"}),
		Entry("name and version", "jq@1.7.1", ToolSpec{Name: "jq", Version: "1.7.1"}),
		Entry("repo with asset hint and version",
			"flanksource/mission-control-plugins#opensearch@v1.8.0",
			ToolSpec{Name: "flanksource/mission-control-plugins", Asset: "opensearch", Version: "v1.8.0"}),
		Entry("repo with asset hint only",
			"flanksource/mission-control-plugins#opensearch",
			ToolSpec{Name: "flanksource/mission-control-plugins", Asset: "opensearch"}),
		Entry("repo pinned to a commit",
			"flanksource/mission-control-plugins#opensearch@"+commit,
			ToolSpec{Name: "flanksource/mission-control-plugins", Asset: "opensearch", Version: commit}),
		Entry("immutable version",
			"flanksource/mission-control-plugins#opensearch@v1.8.0+immutable",
			ToolSpec{Name: "flanksource/mission-control-plugins", Asset: "opensearch", Version: "v1.8.0", Immutable: true}),
		Entry("immutable latest", "owner/repo+immutable", ToolSpec{Name: "owner/repo", Immutable: true}),
		Entry("semver build metadata is kept", "temurin@jdk-21.0.5+11", ToolSpec{Name: "temurin", Version: "jdk-21.0.5+11"}),
	)

	DescribeTable("rejects malformed specs",
		func(input, message string) {
			_, err := ParseToolSpec(input)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("empty asset hint", "owner/repo#@v1.0.0", "empty asset hint"),
		Entry("empty name", "#opensearch@v1.0.0", "empty package name"),
		Entry("empty version", "owner/repo#opensearch@", "empty version"),
		Entry("second version", "owner/repo@v1.0.0@v2.0.0", "more than one @version"),
		Entry("empty version before +immutable", "owner/repo@+immutable", "empty version"),
	)
})

var _ = Describe("parseToolArg", func() {
	It("accepts +immutable on a separately passed version", func() {
		Expect(parseToolArg("owner/repo#opensearch", "v1.8.0+immutable")).To(Equal(
			ToolSpec{Name: "owner/repo", Asset: "opensearch", Version: "v1.8.0", Immutable: true}))
	})
})

var _ = Describe("ParseTools", func() {
	It("returns the first malformed spec as an error", func() {
		_, err := ParseTools([]string{"jq@1.7.1", "owner/repo#@v1.0.0"})
		Expect(err).To(MatchError(ContainSubstring(`"owner/repo#@v1.0.0"`)))
	})
})

var _ = Describe("Installer tool scoping", func() {
	repoPkg := types.Package{Name: "mission-control-plugins", Repo: "flanksource/mission-control-plugins", Manager: "github_release"}

	It("turns the asset hint into the tool's only asset filter", func() {
		scoped, err := New().forTool(ToolSpec{Name: repoPkg.Repo, Asset: "opensearch"})
		Expect(err).NotTo(HaveOccurred())
		Expect(scoped.options.AssetFilters).To(Equal([]string{"opensearch"}))
	})

	It("leaves the shared installer's filters untouched", func() {
		shared := New()
		_, err := shared.forTool(ToolSpec{Name: repoPkg.Repo, Asset: "opensearch"})
		Expect(err).NotTo(HaveOccurred())
		Expect(shared.options.AssetFilters).To(BeEmpty())
	})

	It("rejects an asset hint combined with --asset", func() {
		_, err := New(WithAssetFilters("kubernetes-logs")).forTool(ToolSpec{Name: repoPkg.Repo, Asset: "opensearch"})
		Expect(err).To(MatchError(ContainSubstring("both")))
	})

	It("requires an immutable release for +immutable, only for that tool", func() {
		shared := New()
		scoped, err := shared.forTool(ToolSpec{Name: repoPkg.Repo, Immutable: true})
		Expect(err).NotTo(HaveOccurred())
		Expect([]bool{
			manager.GetRequireImmutable(scoped.managerContext(context.Background())),
			manager.GetRequireImmutable(shared.managerContext(context.Background())),
		}).To(Equal([]bool{true, false}))
	})

	DescribeTable("rejects GitHub-only pinning for other managers",
		func(tool ToolSpec, message string) {
			scoped, err := New().forTool(tool)
			Expect(err).NotTo(HaveOccurred())
			err = scoped.validateGitHubFilters("jq", types.Package{Name: "jq", Manager: "direct"}, tool.Version)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
		Entry("+immutable", ToolSpec{Name: "jq", Version: "1.7.1", Immutable: true}, "+immutable"),
		Entry("commit version", ToolSpec{Name: "jq", Version: "712ce23a27c49bcf4bf161044c8c2203abecadeb"}, "commit"),
	)

	It("accepts pinning for github_release packages", func() {
		scoped, err := New().forTool(ToolSpec{Name: repoPkg.Repo, Immutable: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(scoped.validateGitHubFilters(repoPkg.Name, repoPkg, "712ce23a27c49bcf4bf161044c8c2203abecadeb")).To(Succeed())
	})
})
