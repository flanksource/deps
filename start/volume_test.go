package start

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/start/state"
)

var _ = Describe("primary service volumes", func() {
	It("maps docker volume modes to the service-specific data target", func() {
		svc := &ServiceContext{
			Name: "search", Version: "1.2.3", DataDir: "/srv/search",
			Spec: types.ServiceSpec{
				Ports:  []types.ServicePort{{Name: "http", Port: 9200}},
				Docker: &types.DockerRuntime{Image: "acme/search:{{.version}}", DataPath: "/var/lib/search"},
			},
			Opts: Options{VolumeMode: VolumeHost},
		}
		config, err := (&dockerRuntime{}).DesiredConfig(context.Background(), svc)
		Expect(err).ToNot(HaveOccurred())
		Expect(config.Volume).To(Equal(&state.Volume{Mode: "host", Source: "/srv/search", Target: "/var/lib/search"}))

		svc.Opts.VolumeMode = VolumePersistent
		config, err = (&dockerRuntime{}).DesiredConfig(context.Background(), svc)
		Expect(err).ToNot(HaveOccurred())
		Expect(config.Volume).To(Equal(&state.Volume{Mode: "persistent", Source: "deps-start-search-data", Target: "/var/lib/search"}))

		svc.Opts.VolumeMode = VolumeEphemeral
		config, err = (&dockerRuntime{}).DesiredConfig(context.Background(), svc)
		Expect(err).ToNot(HaveOccurred())
		Expect(config.Volume).To(Equal(&state.Volume{Mode: "ephemeral", Target: "/var/lib/search"}))
	})

	It("preserves an inspected mode when no mode or data dir was supplied", func() {
		svc := &ServiceContext{Spec: types.ServiceSpec{Docker: &types.DockerRuntime{DataPath: "/data"}}}
		live := &state.EffectiveConfig{Volume: &state.Volume{Mode: "persistent", Target: "/data"}}
		Expect(defaultVolumeMode(RuntimeDocker, svc, live)).To(Equal(VolumePersistent))
	})

	It("rejects data-dir with non-host storage", func() {
		_, err := ResolveOptions([]Option{WithDataDir("./data"), WithVolumeMode(VolumeEphemeral)})
		Expect(err).To(MatchError("data-dir can only be used with host volume mode"))
	})

	It("fails when a helm chart does not map the requested mode", func() {
		svc := &ServiceContext{
			Name: "search", DataDir: "/srv/search",
			Spec: types.ServiceSpec{Helm: &types.HelmRuntime{
				Chart: "search",
				Volume: &types.HelmVolume{MountPath: "/data", Modes: map[string]types.HelmVolumeMode{
					"persistent": {Set: map[string]string{"persistence.enabled": "true"}},
				}},
			}},
			Opts: Options{Namespace: "dev", VolumeMode: VolumeHost},
		}
		_, err := (&helmRuntime{}).DesiredConfig(context.Background(), svc)
		Expect(err).To(MatchError("service search helm runtime does not support host volume mode"))
	})
})
