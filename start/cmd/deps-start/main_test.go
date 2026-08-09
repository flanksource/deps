package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"

	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/start"
	"github.com/flanksource/deps/start/state"
)

func TestDepsStart(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "deps-start CLI Suite")
}

var _ = Describe("expandVersionArg", func() {
	It("rewrites name@version into a --version flag", func() {
		Expect(expandVersionArg([]string{"deps-start", "postgres@17", "--port", "15432"})).
			To(Equal([]string{"deps-start", "postgres", "--version", "17", "--port", "15432"}))
	})

	It("supports semver constraints like deps install", func() {
		Expect(expandVersionArg([]string{"deps-start", "nats@>=2.10"})).
			To(Equal([]string{"deps-start", "nats", "--version", ">=2.10"}))
	})

	It("leaves plain service names untouched", func() {
		args := []string{"deps-start", "postgres", "-d"}
		Expect(expandVersionArg(args)).To(Equal(args))
	})

	It("leaves subcommands and empty versions untouched", func() {
		Expect(expandVersionArg([]string{"deps-start", "stop", "postgres"})).
			To(Equal([]string{"deps-start", "stop", "postgres"}))
		Expect(expandVersionArg([]string{"deps-start", "postgres@"})).
			To(Equal([]string{"deps-start", "postgres@"}))
	})
})

var _ = Describe("normalizeArgs", func() {
	It("routes the start verb to the service's own command", func() {
		Expect(normalizeArgs([]string{"deps-start", "start", "postgres", "--port", "15432"})).
			To(Equal([]string{"deps-start", "postgres", "--port", "15432"}))
	})

	It("combines the start verb with name@version", func() {
		Expect(normalizeArgs([]string{"deps-start", "start", "postgres@17", "-d"})).
			To(Equal([]string{"deps-start", "postgres", "--version", "17", "-d"}))
	})

	It("leaves an unknown service for the start command to report", func() {
		args := []string{"deps-start", "start", "not-a-service"}
		Expect(normalizeArgs(args)).To(Equal(args))
	})

	It("leaves start --help and a bare start alone", func() {
		Expect(normalizeArgs([]string{"deps-start", "start", "--help"})).
			To(Equal([]string{"deps-start", "start", "--help"}))
		Expect(normalizeArgs([]string{"deps-start", "start"})).
			To(Equal([]string{"deps-start", "start"}))
	})

	It("does not treat a service named after another verb as a start target", func() {
		args := []string{"deps-start", "stop", "postgres"}
		Expect(normalizeArgs(args)).To(Equal(args))
	})
})

var _ = Describe("service parameter flags", func() {
	It("exposes start alongside the other management verbs", func() {
		root := newRootCmd()
		startCmd, _, err := root.Find([]string{"start"})
		Expect(err).ToNot(HaveOccurred())
		Expect(startCmd.Name()).To(Equal("start"))
		Expect(startCmd.GroupID).To(Equal("management"))
	})

	It("offers --foreground instead of a detach flag, since starting backgrounds by default", func() {
		root := newRootCmd()
		postgres, _, err := root.Find([]string{"postgres"})
		Expect(err).ToNot(HaveOccurred())
		Expect(postgres.Flags().Lookup("foreground")).ToNot(BeNil())
		Expect(postgres.Flags().ShorthandLookup("f")).ToNot(BeNil())
		Expect(postgres.Flags().Lookup("detach")).To(BeNil())
		Expect(postgres.Flags().Lookup("foreground").DefValue).To(Equal("false"))
	})

	It("relaunches a restarted service without a detach flag", func() {
		Expect(restartArgs("nats", &state.StartOptions{Port: 14222}, "")).
			To(Equal([]string{"nats", "--port", "14222"}))
	})

	It("discovers typed OpenSearch and Helm resource flags", func() {
		root := newRootCmd()
		service, _, err := root.Find([]string{"opensearch"})
		Expect(err).ToNot(HaveOccurred())
		Expect(service.Flags().Lookup("jvm-memory")).ToNot(BeNil())
		Expect(service.Flags().Lookup("cpu-request")).ToNot(BeNil())
		Expect(service.Flags().Lookup("memory-limit")).ToNot(BeNil())
	})

	It("keeps parameter values isolated between service commands", func() {
		root := newRootCmd()
		opensearch, _, err := root.Find([]string{"opensearch"})
		Expect(err).ToNot(HaveOccurred())
		nats, _, err := root.Find([]string{"nats"})
		Expect(err).ToNot(HaveOccurred())
		Expect(opensearch.Flags().Set("jvm-memory", "1g")).To(Succeed())
		Expect(nats.Flags().Lookup("jvm-memory")).To(BeNil())
		Expect(nats.Flags().Lookup("jetstream")).ToNot(BeNil())
	})

	It("replays persisted parameters in stable flag order", func() {
		args := restartArgs("nats", &state.StartOptions{
			Parameters: map[string]string{"memory-limit": "1Gi", "jetstream": "false"},
		}, "")
		Expect(strings.Join(args, " ")).To(ContainSubstring("--jetstream=false --memory-limit=1Gi"))
	})

	It("rejects parameter names that collide with common flags", func() {
		cmd := &cobra.Command{}
		flags := &startFlags{}
		cmd.Flags().Int("port", 0, "primary service port override")
		err := addServiceParameterFlags(cmd, flags, map[string]types.ServiceParameter{
			"port": {Type: types.ServiceParameterInt, Description: "conflicting port"},
		})
		Expect(err).To(MatchError(ContainSubstring("collides with an existing flag")))
	})

	It("only supplies service parameters explicitly changed by the user", func() {
		cmd := &cobra.Command{}
		flags := &startFlags{}
		err := addServiceParameterFlags(cmd, flags, map[string]types.ServiceParameter{
			"enabled": {Type: types.ServiceParameterBool, Description: "enable feature", Default: "true"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(flags.parameterValues()).To(BeEmpty())
		Expect(cmd.Flags().Set("enabled", "false")).To(Succeed())
		Expect(flags.parameterValues()).To(Equal(map[string]string{"enabled": "false"}))
	})
})

var _ = Describe("service output", func() {
	It("defaults start output to JSON and includes effective parameters", func() {
		root := newRootCmd()
		opensearch, _, err := root.Find([]string{"opensearch"})
		Expect(err).ToNot(HaveOccurred())
		Expect(opensearch.Flags().Lookup("output").DefValue).To(Equal("json"))

		info := start.ServiceInfo{
			Name:       "opensearch",
			Runtime:    start.RuntimeDocker,
			Status:     state.StatusRunning,
			Parameters: map[string]string{"jvm-memory": "1g"},
		}
		var stdout bytes.Buffer
		Expect(writeServiceOutput(&stdout, info, "json")).To(Succeed())
		var payload map[string]any
		Expect(json.Unmarshal(stdout.Bytes(), &payload)).To(Succeed())
		Expect(payload["parameters"]).To(Equal(map[string]any{"jvm-memory": "1g"}))
	})

	It("renders Clicky configuration changes independently of structured stdout", func() {
		change, err := start.NewConfigChange(
			&state.EffectiveConfig{Runtime: "docker", Image: "acme/search:1"},
			&state.EffectiveConfig{Runtime: "docker", Image: "acme/search:2"},
		)
		Expect(err).ToNot(HaveOccurred())
		var stderr bytes.Buffer
		Expect(writeConfigChange(&stderr, change, false)).To(Succeed())
		Expect(stderr.String()).To(ContainSubstring("-image: acme/search:1"))
		Expect(stderr.String()).To(ContainSubstring("+image: acme/search:2"))
	})
})
