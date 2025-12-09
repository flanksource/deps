package golang

import (
	"context"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GoManager", func() {
	var manager *GoManager

	BeforeEach(func() {
		manager = NewGoManager()
	})

	Describe("Name", func() {
		It("should return the correct manager name", func() {
			Expect(manager.Name()).To(Equal("go"))
		})
	})

	Describe("getImportPath", func() {
		Context("with valid import path", func() {
			It("should return the import path from package extras", func() {
				pkg := types.Package{
					Name: "ginkgo",
					Extra: map[string]interface{}{
						"import_path": "github.com/onsi/ginkgo/v2/ginkgo",
					},
				}
				got, err := manager.getImportPath(pkg)
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal("github.com/onsi/ginkgo/v2/ginkgo"))
			})
		})

		Context("with missing extra", func() {
			It("should return error", func() {
				pkg := types.Package{
					Name: "ginkgo",
				}
				_, err := manager.getImportPath(pkg)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with missing import_path", func() {
			It("should return error", func() {
				pkg := types.Package{
					Name:  "ginkgo",
					Extra: map[string]interface{}{},
				}
				_, err := manager.getImportPath(pkg)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("getBinaryName", func() {
		Context("when import path is present", func() {
			It("should extract binary name from import path", func() {
				pkg := types.Package{
					Name: "ginkgo",
					Extra: map[string]interface{}{
						"import_path": "github.com/onsi/ginkgo/v2/ginkgo",
					},
				}
				got := manager.getBinaryName(pkg)
				Expect(got).To(Equal("ginkgo"))
			})
		})

		Context("when import path is missing", func() {
			It("should fallback to package name", func() {
				pkg := types.Package{
					Name: "mytool",
				}
				got := manager.getBinaryName(pkg)
				Expect(got).To(Equal("mytool"))
			})
		})
	})

	Describe("Resolve", func() {
		It("should resolve a go package version", func() {
			pkg := types.Package{
				Name: "ginkgo",
				Repo: "onsi/ginkgo",
				Extra: map[string]interface{}{
					"import_path": "github.com/onsi/ginkgo/v2/ginkgo",
				},
			}

			plat := platform.Platform{
				OS:   "darwin",
				Arch: "arm64",
			}

			resolution, err := manager.Resolve(context.TODO(), pkg, "v2.23.2", plat)
			Expect(err).NotTo(HaveOccurred())

			// Go packages don't have a download URL - Install() handles everything
			Expect(resolution.DownloadURL).To(BeEmpty())
			Expect(resolution.Version).To(Equal("v2.23.2"))
			Expect(resolution.IsArchive).To(BeFalse())

			// Verify package information is preserved
			Expect(resolution.Package.Name).To(Equal("ginkgo"))
		})
	})
})
