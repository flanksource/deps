package version

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

// mockPackageManager implements PackageManager for testing
type mockPackageManager struct {
	name          string
	versions      []types.Version
	discoverError error
}

func (m *mockPackageManager) Name() string {
	return m.name
}

func (m *mockPackageManager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	if m.discoverError != nil {
		return nil, m.discoverError
	}

	versions := m.versions
	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}
	return versions, nil
}

var _ = Describe("SortVersions", func() {
	It("should sort versions with dots before versions without dots", func() {
		versions := []types.Version{
			{Version: "20250101"},
			{Version: "4.1.0"},
			{Version: "4"},
			{Version: "3.0.0"},
			{Version: "20240601"},
		}

		SortVersions(versions)

		// Versions with dots should come first
		Expect(versions[0].Version).To(Equal("4.1.0"))
		Expect(versions[1].Version).To(Equal("3.0.0"))
		// Versions without dots come after
		Expect(versions[2].Version).To(Equal("20250101"))
		Expect(versions[3].Version).To(Equal("20240601"))
		Expect(versions[4].Version).To(Equal("4"))
	})

	It("should sort semver versions in descending order", func() {
		versions := []types.Version{
			{Version: "1.0.0"},
			{Version: "2.0.0"},
			{Version: "1.5.0"},
		}

		SortVersions(versions)

		Expect(versions[0].Version).To(Equal("2.0.0"))
		Expect(versions[1].Version).To(Equal("1.5.0"))
		Expect(versions[2].Version).To(Equal("1.0.0"))
	})

	It("should handle mixed semver and non-semver versions", func() {
		versions := []types.Version{
			{Version: "20250110"},
			{Version: "4.1.0"},
			{Version: "20250101"},
			{Version: "4.0.0"},
		}

		SortVersions(versions)

		// Dotted versions first (descending)
		Expect(versions[0].Version).To(Equal("4.1.0"))
		Expect(versions[1].Version).To(Equal("4.0.0"))
		// Non-dotted versions after (descending)
		Expect(versions[2].Version).To(Equal("20250110"))
		Expect(versions[3].Version).To(Equal("20250101"))
	})

	It("should sort versions with build metadata numerically", func() {
		// Versions like OpenJDK: 17.0.17+10, 17.0.9+9.1, 17.0.8.1+1
		versions := []types.Version{
			{Version: "17.0.9+9.1"},
			{Version: "17.0.17+10"},
			{Version: "17.0.8.1+1"},
			{Version: "17.0.10+7"},
		}

		SortVersions(versions)

		// Should sort by numeric version parts, ignoring build metadata
		Expect(versions[0].Version).To(Equal("17.0.17+10"))
		Expect(versions[1].Version).To(Equal("17.0.10+7"))
		Expect(versions[2].Version).To(Equal("17.0.9+9.1"))
		Expect(versions[3].Version).To(Equal("17.0.8.1+1"))
	})

	It("should sort 4-part versions correctly", func() {
		versions := []types.Version{
			{Version: "17.0.4.1"},
			{Version: "17.0.8.1"},
			{Version: "17.0.8"},
			{Version: "17.0.5"},
		}

		SortVersions(versions)

		Expect(versions[0].Version).To(Equal("17.0.8.1"))
		Expect(versions[1].Version).To(Equal("17.0.8"))
		Expect(versions[2].Version).To(Equal("17.0.5"))
		Expect(versions[3].Version).To(Equal("17.0.4.1"))
	})
})

var _ = Describe("Version Resolver", func() {
	var testVersions []types.Version

	BeforeEach(func() {
		testVersions = []types.Version{
			{Tag: "v2.1.0", Version: "2.1.0", Prerelease: false},
			{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
			{Tag: "v1.5.0", Version: "1.5.0", Prerelease: false},
			{Tag: "v1.4.0", Version: "1.4.0", Prerelease: false},
			{Tag: "v1.3.0-beta", Version: "1.3.0-beta", Prerelease: true},
		}
	})

	Describe("ResolveConstraint", func() {
		Context("with valid constraints", func() {
			It("should resolve latest constraint to newest stable version", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: testVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "latest", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v2.1.0"))
			})

			It("should resolve stable constraint to newest stable version", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: testVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "stable", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v2.1.0"))
			})

			It("should resolve any constraint same as latest", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: testVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "any", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v2.1.0"))
			})

			It("should resolve exact version constraint", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: testVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "v1.5.0", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v1.5.0"))
			})

			It("should resolve exact version without v prefix", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: testVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "1.5.0", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v1.5.0"))
			})

			It("should resolve semver constraint ^1.0.0", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: testVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "^1.0.0", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v1.5.0"))
			})

			It("should resolve semver constraint ~1.4.0", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: testVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "~1.4.0", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v1.4.0"))
			})
		})

		Context("with partial version constraints", func() {
			var extendedVersions []types.Version

			BeforeEach(func() {
				extendedVersions = []types.Version{
					{Tag: "v3.1.2", Version: "3.1.2", Prerelease: false},
					{Tag: "v3.1.1", Version: "3.1.1", Prerelease: false},
					{Tag: "v3.1.0", Version: "3.1.0", Prerelease: false},
					{Tag: "v3.0.5", Version: "3.0.5", Prerelease: false},
					{Tag: "v3.0.0", Version: "3.0.0", Prerelease: false},
					{Tag: "v2.1.0", Version: "2.1.0", Prerelease: false},
					{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
					{Tag: "v1.5.3", Version: "1.5.3", Prerelease: false},
					{Tag: "v1.5.0", Version: "1.5.0", Prerelease: false},
					{Tag: "v1.4.0", Version: "1.4.0", Prerelease: false},
					{Tag: "v1.3.0-beta", Version: "1.3.0-beta", Prerelease: true},
				}
			})

			It("should resolve major version constraint (3)", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: extendedVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "3", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v3.1.2")) // Latest 3.x.x
			})

			It("should resolve major version constraint with v prefix (v2)", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: extendedVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "v2", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v2.1.0")) // Latest 2.x.x
			})

			It("should resolve major.minor version constraint (1.5)", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: extendedVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "1.5", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v1.5.3")) // Latest 1.5.x
			})

			It("should resolve major.minor version constraint with v prefix (v3.0)", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: extendedVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "v3.0", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v3.0.5")) // Latest 3.0.x
			})

			It("should skip prereleases in partial version resolution", func() {
				versionsWithPrerelease := []types.Version{
					{Tag: "v1.5.4-beta", Version: "1.5.4-beta", Prerelease: true},
					{Tag: "v1.5.3", Version: "1.5.3", Prerelease: false},
					{Tag: "v1.5.0", Version: "1.5.0", Prerelease: false},
				}

				mgr := &mockPackageManager{
					name:     "test",
					versions: versionsWithPrerelease,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "1.5", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v1.5.3")) // Skip prerelease, use latest stable
			})

			It("should use prerelease if no stable versions match", func() {
				onlyPrereleaseVersions := []types.Version{
					{Tag: "v2.0.0-beta", Version: "2.0.0-beta", Prerelease: true},
					{Tag: "v1.5.0", Version: "1.5.0", Prerelease: false},
				}

				mgr := &mockPackageManager{
					name:     "test",
					versions: onlyPrereleaseVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "2", plat)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v2.0.0-beta")) // Use prerelease when no stable available
			})

			It("should return error when no versions match partial constraint", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: extendedVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "5", plat)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no versions found matching pattern 5"))
				Expect(result).To(Equal(""))
			})
		})

		Context("with error conditions", func() {
			It("should return error when stable constraint has no stable versions", func() {
				prereleaseOnlyVersions := []types.Version{
					{Tag: "v1.0.0-beta", Version: "1.0.0-beta", Prerelease: true},
				}

				mgr := &mockPackageManager{
					name:     "test",
					versions: prereleaseOnlyVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "stable", plat)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no stable versions found"))
				Expect(result).To(Equal(""))
			})

			It("should return error when exact version not found", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: testVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "v3.0.0", plat)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Version v3.0.0 not found"))
				Expect(result).To(Equal(""))
			})

			It("should return error with empty constraint", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: testVersions,
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "", plat)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("empty version constraint"))
				Expect(result).To(Equal(""))
			})

			It("should return error when no versions available", func() {
				mgr := &mockPackageManager{
					name:     "test",
					versions: []types.Version{},
				}

				resolver := NewResolver(mgr)
				pkg := types.Package{Name: "test-pkg"}
				plat := platform.Platform{OS: "linux", Arch: "amd64"}

				result, err := resolver.ResolveConstraint(context.Background(), pkg, "latest", plat)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no versions found"))
				Expect(result).To(Equal(""))
			})
		})
	})

	Describe("GetOptimalLimit", func() {
		var resolver *VersionResolver

		BeforeEach(func() {
			mgr := &mockPackageManager{name: "test"}
			resolver = NewResolver(mgr)
		})

		DescribeTable("should return optimal limits for different constraints",
			func(constraint string, expectedLimit int) {
				result := resolver.getOptimalLimit(constraint)
				Expect(result).To(Equal(expectedLimit))
			},
			Entry("latest constraint", "latest", 10),
			Entry("any constraint", "any", 10),
			Entry("stable constraint", "stable", 20),
			Entry("exact version with v", "v1.2.3", 200),
			Entry("exact version without v", "1.2.3", 200),
			Entry("caret constraint", "^1.0.0", 100),
			Entry("tilde constraint", "~1.2.0", 50),
			Entry("greater than constraint", ">=1.0.0", 100),
			Entry("range constraint", ">=1.0.0 <2.0.0", 50),
			Entry("unknown constraint", "unknown", 50),
		)
	})

	Describe("SelectBestVersion", func() {
		var resolver *VersionResolver

		BeforeEach(func() {
			mgr := &mockPackageManager{name: "test"}
			resolver = NewResolver(mgr)
		})

		Context("with valid scenarios", func() {
			It("should select latest with stable versions", func() {
				result, err := resolver.selectBestVersion(types.Package{}, testVersions, "latest")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v2.1.0"))
			})

			It("should treat any constraint same as latest", func() {
				result, err := resolver.selectBestVersion(types.Package{}, testVersions, "any")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v2.1.0"))
			})

			It("should select latest with only prerelease", func() {
				prereleaseVersions := []types.Version{
					{Tag: "v1.0.0-beta", Version: "1.0.0-beta", Prerelease: true},
				}
				result, err := resolver.selectBestVersion(types.Package{}, prereleaseVersions, "latest")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v1.0.0-beta"))
			})

			It("should select exact version match", func() {
				result, err := resolver.selectBestVersion(types.Package{}, testVersions, "v1.5.0")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v1.5.0"))
			})

			It("should select semver constraint", func() {
				result, err := resolver.selectBestVersion(types.Package{}, testVersions, "^1.0.0")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v1.5.0"))
			})
		})

		Context("with error conditions", func() {
			It("should return error with no versions", func() {
				result, err := resolver.selectBestVersion(types.Package{}, []types.Version{}, "latest")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no versions available"))
				Expect(result).To(Equal(""))
			})
		})
	})

	Describe("GetLatestVersion", func() {
		var resolver *VersionResolver

		BeforeEach(func() {
			mgr := &mockPackageManager{name: "test"}
			resolver = NewResolver(mgr)
		})

		Context("with mixed version types", func() {
			var mixedVersions []types.Version

			BeforeEach(func() {
				mixedVersions = []types.Version{
					{Tag: "v2.1.0", Version: "2.1.0", Prerelease: false},
					{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
					{Tag: "v1.5.0-beta", Version: "1.5.0-beta", Prerelease: true},
					{Tag: "v1.4.0", Version: "1.4.0", Prerelease: false},
				}
			})

			It("should return latest stable only", func() {
				result := resolver.getLatestVersion(mixedVersions, true)
				Expect(result).To(Equal("v2.1.0"))
			})

			It("should return latest including prereleases", func() {
				result := resolver.getLatestVersion(mixedVersions, false)
				Expect(result).To(Equal("v2.1.0"))
			})
		})

		Context("with only prereleases", func() {
			var prereleaseVersions []types.Version

			BeforeEach(func() {
				prereleaseVersions = []types.Version{
					{Tag: "v1.0.0-beta", Version: "1.0.0-beta", Prerelease: true},
				}
			})

			It("should return empty string with stable only", func() {
				result := resolver.getLatestVersion(prereleaseVersions, true)
				Expect(result).To(Equal(""))
			})

			It("should return prerelease without stable only", func() {
				result := resolver.getLatestVersion(prereleaseVersions, false)
				Expect(result).To(Equal("v1.0.0-beta"))
			})
		})

		Context("with empty versions", func() {
			It("should return empty string", func() {
				result := resolver.getLatestVersion([]types.Version{}, false)
				Expect(result).To(Equal(""))
			})
		})
	})

	Describe("FindExactVersion", func() {
		var resolver *VersionResolver
		var testVersionsWithoutV []types.Version

		BeforeEach(func() {
			mgr := &mockPackageManager{name: "test"}
			resolver = NewResolver(mgr)

			testVersionsWithoutV = []types.Version{
				{Tag: "v2.1.0", Version: "2.1.0", Prerelease: false},
				{Tag: "v2.0.0", Version: "2.0.0", Prerelease: false},
				{Tag: "1.5.0", Version: "1.5.0", Prerelease: false}, // No v prefix
			}
		})

		Context("with successful matches", func() {
			It("should match exact tag with v prefix", func() {
				result, err := resolver.findExactVersion(types.Package{}, testVersionsWithoutV, "v2.1.0")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v2.1.0"))
			})

			It("should match version without v prefix", func() {
				result, err := resolver.findExactVersion(types.Package{}, testVersionsWithoutV, "2.1.0")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("v2.1.0"))
			})

			It("should match tag without v prefix", func() {
				result, err := resolver.findExactVersion(types.Package{}, testVersionsWithoutV, "1.5.0")
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal("1.5.0"))
			})
		})

		Context("when version not found", func() {
			It("should return error", func() {
				result, err := resolver.findExactVersion(types.Package{}, testVersionsWithoutV, "v3.0.0")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Version v3.0.0 not found"))
				Expect(result).To(Equal(""))
			})
		})
	})
})
