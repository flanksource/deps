package start

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

const rateLimitedManagerName = "docker-version-test-rate-limited"

var errRateLimited = errors.New("HTTP 429")

// rateLimitedManager fails every version lookup, standing in for a binary
// package registry (e.g. Maven Central) that rate-limits the caller.
type rateLimitedManager struct{ manager.PackageManager }

func (rateLimitedManager) Name() string { return rateLimitedManagerName }

func (rateLimitedManager) DiscoverVersions(context.Context, types.Package, platform.Platform, int) ([]types.Version, error) {
	return nil, errRateLimited
}

var _ = Describe("docker runtime version", func() {
	BeforeEach(func() {
		manager.GetGlobalRegistry().Register(rateLimitedManager{})
	})

	dockerService := func(version string) *ServiceContext {
		return &ServiceContext{
			Name: "postgres", Version: version,
			Package: types.Package{Name: "postgres", Manager: rateLimitedManagerName},
			Spec: types.ServiceSpec{
				Ports:  []types.ServicePort{{Name: "postgres", Port: 5432}},
				Docker: &types.DockerRuntime{Image: "postgres:{{.major}}"},
			},
		}
	}

	DescribeTable("uses a concrete version as the image tag without consulting the binary package manager",
		func(version, image string) {
			config, err := (&dockerRuntime{}).DesiredConfig(context.Background(), dockerService(version))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Version).To(Equal(version))
			Expect(config.Image).To(Equal(image))
		},
		Entry("major", "16", "postgres:16"),
		Entry("major.minor", "16.4", "postgres:16"),
		Entry("exact", "16.4.1", "postgres:16"),
	)

	DescribeTable("resolves non-concrete constraints through the package manager",
		func(constraint string) {
			_, err := (&dockerRuntime{}).DesiredConfig(context.Background(), dockerService(constraint))
			Expect(err).To(MatchError(errRateLimited))
		},
		Entry("latest", "latest"),
		Entry("range", ">=16"),
	)
})
