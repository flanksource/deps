package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	// Import package managers to register them
	_ "github.com/flanksource/deps/pkg/manager/apache"
	_ "github.com/flanksource/deps/pkg/manager/direct"
	_ "github.com/flanksource/deps/pkg/manager/github"
	_ "github.com/flanksource/deps/pkg/manager/gitlab"
	_ "github.com/flanksource/deps/pkg/manager/maven"
	_ "github.com/flanksource/deps/pkg/manager/url"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	// Global setup before all tests
	GinkgoLogr.Info("Starting E2E test suite for deps platform lock functionality")
})

var _ = AfterSuite(func() {
	// Global cleanup after all tests
	GinkgoLogr.Info("E2E test suite completed")
})
