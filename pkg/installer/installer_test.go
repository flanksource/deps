package installer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/types"
)

var _ = Describe("Installer", func() {
	Describe("shouldSkipCleanup", func() {
		It("should return false when using default temp directory", func() {
			inst := New(WithTmpDir(os.TempDir()))
			Expect(inst.shouldSkipCleanup()).To(BeFalse())
		})

		It("should return true when using custom temp directory", func() {
			customTmpDir, err := os.MkdirTemp("", "custom-tmp-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(customTmpDir) }()

			inst := New(WithTmpDir(customTmpDir))
			Expect(inst.shouldSkipCleanup()).To(BeTrue())
		})
	})

	Describe("WithTmpDir option", func() {
		It("should set the TmpDir option correctly", func() {
			customTmpDir := "/custom/tmp/dir"
			inst := New(WithTmpDir(customTmpDir))
			Expect(inst.options.TmpDir).To(Equal(customTmpDir))
		})

		It("should default to os.TempDir() when not specified", func() {
			inst := New()
			Expect(inst.options.TmpDir).To(Equal(os.TempDir()))
		})
	})

	Describe("GitHub filter options", func() {
		It("stores independent copies of asset and release filters", func() {
			assetFilters := []string{"cli", "!*debug*"}
			releaseFilters := []string{"v2*", "!v2.0.0-beta*"}
			inst := New(WithAssetFilters(assetFilters...), WithReleaseFilters(releaseFilters...))
			assetFilters[0] = "changed"
			releaseFilters[0] = "changed"

			Expect(inst.options.AssetFilters).To(Equal([]string{"cli", "!*debug*"}))
			Expect(inst.options.ReleaseFilters).To(Equal([]string{"v2*", "!v2.0.0-beta*"}))
		})

		It("passes both filters to package manager resolution", func() {
			inst := New(WithAssetFilters("cli*"), WithReleaseFilters("v2*"))
			ctx := inst.managerContext(context.Background())

			Expect(manager.GetAssetFilters(ctx)).To(Equal([]string{"cli*"}))
			Expect(manager.GetReleaseFilters(ctx)).To(Equal([]string{"v2*"}))
		})

		It("rejects filters for managers that cannot apply them", func() {
			inst := New(WithAssetFilters("cli*"))
			Expect(inst.validateGitHubFilters("tool", types.Package{Name: "tool", Manager: "url"})).To(MatchError("--asset and --release-filter require the github_release manager, got url for tool"))
		})
	})

	Describe("TmpDir integration", func() {
		It("should use the specified tmp directory for temporary operations", func() {
			// Create a custom tmp directory
			customTmpDir, err := os.MkdirTemp("", "deps-test-tmp-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(customTmpDir) }()

			// Create installer with custom tmp dir
			inst := New(WithTmpDir(customTmpDir))

			// Verify the configuration
			Expect(inst.options.TmpDir).To(Equal(customTmpDir))
			Expect(inst.shouldSkipCleanup()).To(BeTrue())

			// Verify directory exists and is writable
			testFile := filepath.Join(customTmpDir, "test-write")
			err = os.WriteFile(testFile, []byte("test"), 0644)
			Expect(err).NotTo(HaveOccurred())

			// Clean up test file
			_ = os.Remove(testFile)
		})
	})
})

func TestTmpDirFunctionality(t *testing.T) {
	// Unit test for shouldSkipCleanup logic
	t.Run("shouldSkipCleanup returns correct values", func(t *testing.T) {
		// Test with default temp dir
		inst1 := New(WithTmpDir(os.TempDir()))
		if inst1.shouldSkipCleanup() {
			t.Error("shouldSkipCleanup should return false for default temp dir")
		}

		// Test with custom temp dir
		customTmpDir, err := os.MkdirTemp("", "custom-tmp-*")
		if err != nil {
			t.Fatalf("Failed to create custom temp dir: %v", err)
		}
		defer func() { _ = os.RemoveAll(customTmpDir) }()

		inst2 := New(WithTmpDir(customTmpDir))
		if !inst2.shouldSkipCleanup() {
			t.Error("shouldSkipCleanup should return true for custom temp dir")
		}
	})
}
