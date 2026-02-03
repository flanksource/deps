package manager

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

// mockReleaseIterator implements ReleaseIterator for testing
type mockReleaseIterator struct {
	releases      []ReleaseInfo
	resolveErrors map[string]error // tag -> error
	resolvedTags  []string         // tracks which tags were tried
}

func (m *mockReleaseIterator) FetchReleases(ctx context.Context, limit int) ([]ReleaseInfo, error) {
	if limit > 0 && limit < len(m.releases) {
		return m.releases[:limit], nil
	}
	return m.releases, nil
}

func (m *mockReleaseIterator) TryResolve(ctx context.Context, release ReleaseInfo) (*types.Resolution, error) {
	m.resolvedTags = append(m.resolvedTags, release.Tag)
	if err, exists := m.resolveErrors[release.Tag]; exists {
		return nil, err
	}
	return &types.Resolution{
		Package:     types.Package{Name: "test-pkg"},
		Version:     release.Version,
		Platform:    platform.Platform{OS: "linux", Arch: "amd64"},
		DownloadURL: fmt.Sprintf("https://example.com/releases/%s/test.tar.gz", release.Tag),
	}, nil
}

var _ = Describe("Release Iteration", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("IterateReleasesForAsset", func() {
		Context("when first release has matching assets", func() {
			It("should return resolution immediately", func() {
				iterator := &mockReleaseIterator{
					releases: []ReleaseInfo{
						{Tag: "v1.0.0", Version: "1.0.0", PublishedAt: time.Now()},
						{Tag: "v0.9.0", Version: "0.9.0", PublishedAt: time.Now().Add(-24 * time.Hour)},
					},
					resolveErrors: map[string]error{},
				}

				resolution, err := IterateReleasesForAsset(ctx, iterator, 5)
				Expect(err).NotTo(HaveOccurred())
				Expect(resolution).NotTo(BeNil())
				Expect(resolution.Version).To(Equal("1.0.0"))
				Expect(iterator.resolvedTags).To(HaveLen(1))
				Expect(iterator.resolvedTags[0]).To(Equal("v1.0.0"))
			})
		})

		Context("when first release has no assets", func() {
			It("should try next release", func() {
				iterator := &mockReleaseIterator{
					releases: []ReleaseInfo{
						{Tag: "v1.0.0", Version: "1.0.0", PublishedAt: time.Now()},
						{Tag: "v0.9.0", Version: "0.9.0", PublishedAt: time.Now().Add(-24 * time.Hour)},
						{Tag: "v0.8.0", Version: "0.8.0", PublishedAt: time.Now().Add(-48 * time.Hour)},
					},
					resolveErrors: map[string]error{
						"v1.0.0": &ErrAssetNotFound{Package: "test", Platform: "linux-amd64"},
					},
				}

				resolution, err := IterateReleasesForAsset(ctx, iterator, 5)
				Expect(err).NotTo(HaveOccurred())
				Expect(resolution).NotTo(BeNil())
				Expect(resolution.Version).To(Equal("0.9.0"))
				Expect(iterator.resolvedTags).To(HaveLen(2))
				Expect(iterator.resolvedTags).To(Equal([]string{"v1.0.0", "v0.9.0"}))
			})
		})

		Context("when multiple releases have no assets", func() {
			It("should continue until finding one with assets", func() {
				iterator := &mockReleaseIterator{
					releases: []ReleaseInfo{
						{Tag: "v1.0.0", Version: "1.0.0"},
						{Tag: "v0.9.0", Version: "0.9.0"},
						{Tag: "v0.8.0", Version: "0.8.0"},
						{Tag: "v0.7.0", Version: "0.7.0"},
					},
					resolveErrors: map[string]error{
						"v1.0.0": &ErrAssetNotFound{Package: "test"},
						"v0.9.0": &ErrAssetNotFound{Package: "test"},
						"v0.8.0": &ErrAssetNotFound{Package: "test"},
					},
				}

				resolution, err := IterateReleasesForAsset(ctx, iterator, 5)
				Expect(err).NotTo(HaveOccurred())
				Expect(resolution).NotTo(BeNil())
				Expect(resolution.Version).To(Equal("0.7.0"))
				Expect(iterator.resolvedTags).To(HaveLen(4))
			})
		})

		Context("when all releases have no assets", func() {
			It("should return enhanced error with tried versions", func() {
				iterator := &mockReleaseIterator{
					releases: []ReleaseInfo{
						{Tag: "v1.0.0", Version: "1.0.0"},
						{Tag: "v0.9.0", Version: "0.9.0"},
					},
					resolveErrors: map[string]error{
						"v1.0.0": &ErrAssetNotFound{Package: "test"},
						"v0.9.0": &ErrAssetNotFound{Package: "test"},
					},
				}

				resolution, err := IterateReleasesForAsset(ctx, iterator, 5)
				Expect(resolution).To(BeNil())
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("v1.0.0"))
				Expect(err.Error()).To(ContainSubstring("v0.9.0"))
				Expect(err.Error()).To(ContainSubstring("2 releases"))
			})
		})

		Context("when non-asset error occurs", func() {
			It("should return error immediately without trying more releases", func() {
				networkErr := fmt.Errorf("network error")
				iterator := &mockReleaseIterator{
					releases: []ReleaseInfo{
						{Tag: "v1.0.0", Version: "1.0.0"},
						{Tag: "v0.9.0", Version: "0.9.0"},
					},
					resolveErrors: map[string]error{
						"v1.0.0": networkErr,
					},
				}

				resolution, err := IterateReleasesForAsset(ctx, iterator, 5)
				Expect(resolution).To(BeNil())
				Expect(err).To(MatchError(networkErr))
				Expect(iterator.resolvedTags).To(HaveLen(1))
			})
		})

		Context("with max iterations limit", func() {
			It("should respect the limit", func() {
				iterator := &mockReleaseIterator{
					releases: []ReleaseInfo{
						{Tag: "v5.0.0"},
						{Tag: "v4.0.0"},
						{Tag: "v3.0.0"},
						{Tag: "v2.0.0"},
						{Tag: "v1.0.0"},
					},
					resolveErrors: map[string]error{
						"v5.0.0": &ErrAssetNotFound{Package: "test"},
						"v4.0.0": &ErrAssetNotFound{Package: "test"},
						"v3.0.0": &ErrAssetNotFound{Package: "test"},
						"v2.0.0": &ErrAssetNotFound{Package: "test"},
						"v1.0.0": &ErrAssetNotFound{Package: "test"},
					},
				}

				resolution, err := IterateReleasesForAsset(ctx, iterator, 3)
				Expect(resolution).To(BeNil())
				Expect(err).To(HaveOccurred())
				Expect(iterator.resolvedTags).To(HaveLen(3))
			})
		})

		Context("with no releases", func() {
			It("should return error", func() {
				iterator := &mockReleaseIterator{
					releases:      []ReleaseInfo{},
					resolveErrors: map[string]error{},
				}

				resolution, err := IterateReleasesForAsset(ctx, iterator, 5)
				Expect(resolution).To(BeNil())
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no releases found"))
			})
		})
	})

	Describe("IsAssetNotFoundError", func() {
		It("should return true for ErrAssetNotFound", func() {
			err := &ErrAssetNotFound{Package: "test", Platform: "linux-amd64"}
			Expect(IsAssetNotFoundError(err)).To(BeTrue())
		})

		It("should return true for wrapped ErrAssetNotFound", func() {
			err := fmt.Errorf("failed: %w", &ErrAssetNotFound{Package: "test"})
			Expect(IsAssetNotFoundError(err)).To(BeTrue())
		})

		It("should return false for other errors", func() {
			err := fmt.Errorf("network error")
			Expect(IsAssetNotFoundError(err)).To(BeFalse())
		})

		It("should return false for nil", func() {
			Expect(IsAssetNotFoundError(nil)).To(BeFalse())
		})
	})

	Describe("FilterNonPrereleases", func() {
		It("should filter out prereleases", func() {
			releases := []ReleaseInfo{
				{Tag: "v1.0.0", IsPrerelease: false},
				{Tag: "v1.1.0-rc1", IsPrerelease: true},
				{Tag: "v1.1.0-beta", IsPrerelease: true},
				{Tag: "v0.9.0", IsPrerelease: false},
			}

			filtered := FilterNonPrereleases(releases)
			Expect(filtered).To(HaveLen(2))
			Expect(filtered[0].Tag).To(Equal("v1.0.0"))
			Expect(filtered[1].Tag).To(Equal("v0.9.0"))
		})

		It("should return empty slice when all are prereleases", func() {
			releases := []ReleaseInfo{
				{Tag: "v1.0.0-rc1", IsPrerelease: true},
				{Tag: "v1.0.0-beta", IsPrerelease: true},
			}

			filtered := FilterNonPrereleases(releases)
			Expect(filtered).To(BeEmpty())
		})

		It("should handle empty input", func() {
			filtered := FilterNonPrereleases([]ReleaseInfo{})
			Expect(filtered).To(BeEmpty())
		})
	})

	Describe("Context functions", func() {
		It("should store and retrieve iterate versions from context", func() {
			ctx := context.Background()
			Expect(GetIterateVersions(ctx)).To(Equal(0))

			ctx = WithIterateVersions(ctx, 5)
			Expect(GetIterateVersions(ctx)).To(Equal(5))

			ctx = WithIterateVersions(ctx, 10)
			Expect(GetIterateVersions(ctx)).To(Equal(10))
		})
	})
})
