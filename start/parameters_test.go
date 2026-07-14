package start

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
)

var _ = Describe("service parameters", func() {
	spec := func() types.ServiceSpec {
		return types.ServiceSpec{
			Binary: &types.BinaryRuntime{Command: "server"},
			Helm:   &types.HelmRuntime{Chart: "server"},
			Parameters: map[string]types.ServiceParameter{
				"jetstream": {
					Type:        types.ServiceParameterBool,
					Description: "enable persistence",
					Default:     "true",
				},
				"memory-request": {
					Type:        types.ServiceParameterQuantity,
					Description: "Kubernetes memory request",
					Runtimes:    []string{"helm"},
				},
				"wait": {
					Type:        types.ServiceParameterDuration,
					Description: "service wait duration",
					Default:     "90s",
				},
			},
		}
	}

	It("resolves typed defaults and canonical supplied values", func() {
		values, persisted, err := resolveServiceParameters(spec(), RuntimeHelm, map[string]string{
			"jetstream":      "false",
			"memory-request": "1024Mi",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(values).To(Equal(map[string]any{
			"jetstream":      false,
			"memory-request": "1024Mi",
			"wait":           90 * time.Second,
		}))
		Expect(persisted).To(Equal(map[string]string{
			"jetstream":      "false",
			"memory-request": "1024Mi",
			"wait":           "1m30s",
		}))
	})

	It("rejects invalid values before starting a runtime", func() {
		_, _, err := resolveServiceParameters(spec(), RuntimeHelm, map[string]string{"memory-request": "not-a-quantity"})
		Expect(err).To(MatchError(ContainSubstring("memory-request")))
	})

	It("validates values before creating service state", func() {
		stateDir := filepath.Join(GinkgoT().TempDir(), "state")
		_, err := Start(context.Background(), "opensearch",
			WithRuntime(RuntimeHelm),
			WithStateDir(stateDir),
			WithParameters(map[string]string{"memory-limit": "invalid"}),
		)
		Expect(err).To(MatchError(ContainSubstring("memory-limit")))
		_, statErr := os.Stat(stateDir)
		Expect(os.IsNotExist(statErr)).To(BeTrue())
	})

	It("rejects a supplied parameter for the selected runtime", func() {
		_, _, err := resolveServiceParameters(spec(), RuntimeBinary, map[string]string{"memory-request": "512Mi"})
		Expect(err).To(MatchError(ContainSubstring("only applies to helm")))
	})

	It("rejects invalid parameter definitions before resolving values", func() {
		invalid := spec()
		invalid.Parameters["Memory_Request"] = types.ServiceParameter{
			Type:        types.ServiceParameterQuantity,
			Description: "invalid flag name",
		}
		_, _, err := resolveServiceParameters(invalid, RuntimeHelm, nil)
		Expect(err).To(MatchError(ContainSubstring("invalid parameter name")))
	})

	It("exposes typed values to service templates", func() {
		svc := testContext(spec(), Options{})
		svc.Parameters = map[string]any{"jetstream": false}
		data := templateData(svc, "localhost:4222", "")
		Expect(data["parameters"]).To(Equal(map[string]any{"jetstream": false}))
	})

	It("rejects an invalid primary port", func() {
		_, err := ResolveOptions([]Option{WithPort(65536)})
		Expect(err).To(MatchError(ContainSubstring("port")))
	})
})
