package start

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/start/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const siloTag = "RELEASE.2026-10-26T00-00-00Z"

var _ = Describe("Silo catalog", func() {
	It("renders credentials, region, console and chart settings", func() {
		_, spec, ok := config.GetService("silo")
		Expect(ok).To(BeTrue())
		parameters, _, err := resolveServiceParameters(*spec, RuntimeHelm, nil)
		Expect(err).NotTo(HaveOccurred())
		svc := testContext(*spec, Options{Namespace: "dev", Port: 19000, WaitTimeout: time.Minute})
		svc.Name, svc.Tag, svc.Version = "silo", siloTag, "20261026000000.0.0"
		svc.Parameters, svc.RunDir = parameters, GinkgoT().TempDir()
		release, data, err := helmRelease(svc)
		Expect(err).NotTo(HaveOccurred())
		args, err := buildHelmArgs(svc, data, release)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(ContainElements("image.tag="+siloTag, "mode=standalone", "service.port=19000", "persistence.enabled=true"))
		conn, err := BuildConnection(svc, &state.State{HelmRelease: release}, RuntimeHelm)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn.URL).To(Equal("svc://deps-silo.dev:19000"))
		Expect(conn.Password).To(Equal("secret://deps-silo/rootPassword"))
		Expect(conn.Properties).To(HaveKeyWithValue("console_url", "http://deps-silo-console.dev:9001"))
	})
	It("rejects an unverified Helm source", func() {
		_, spec, _ := config.GetService("silo")
		copy := *spec.Helm.Source
		copy.SHA256 = "invalid"
		helm := *spec.Helm
		helm.Source = &copy
		svc := testContext(*spec, Options{})
		svc.Spec.Helm = &helm
		_, err := prepareHelmChart(context.Background(), svc)
		Expect(err).To(MatchError(ContainSubstring("SHA-256")))
	})
})

var _ = Describe("Silo live", Label("integration"), func() {
	DescribeTable("stores objects and survives restart",
		func(kind RuntimeKind) {
			ctx := context.Background()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			port := listener.Addr().(*net.TCPAddr).Port
			Expect(listener.Close()).To(Succeed())
			opts := []Option{WithStateDir(GinkgoT().TempDir()), WithRuntime(kind), WithPort(port), WithVersion(siloTag), WithWaitTimeout(3 * time.Minute)}
			if kind == RuntimeDocker {
				instanceID := fmt.Sprint(time.Now().Unix())
				opts = append(opts, WithInstanceID(instanceID), WithVolumeMode(VolumePersistent))
				DeferCleanup(func() {
					Expect(exec.Command("docker", "volume", "rm", containerName("silo", instanceID)+"-data").Run()).To(Succeed())
				})
			}
			instance, err := Start(ctx, "silo", opts...)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(Stop(ctx, "silo", opts...)).To(Succeed()) })
			reused, err := Start(ctx, "silo", opts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(reused.Connection.URL).To(Equal(instance.Connection.URL))
			Expect(reused.Connection.Username).To(Equal(instance.Connection.Username))
			Expect(reused.Connection.Password).To(Equal(instance.Connection.Password))
			Expect(reused.Connection.Properties).To(Equal(instance.Connection.Properties))
			Expect(reused.State.PID).To(Equal(instance.State.PID))
			Expect(reused.State.ContainerID).To(Equal(instance.State.ContainerID))
			request := func(method, path, body string) string {
				args := []string{"--fail", "--silent", "--show-error", "--aws-sigv4", "aws:amz:us-east-1:s3", "--user", instance.Connection.Username + ":" + instance.Connection.Password, "-X", method, instance.Connection.URL + path}
				if body != "" {
					args = append(args, "--data-binary", body)
				}
				output, requestErr := exec.CommandContext(ctx, "curl", args...).CombinedOutput()
				Expect(requestErr).NotTo(HaveOccurred(), string(output))
				return string(output)
			}
			request("PUT", "/deps-fixture", "")
			request("PUT", "/deps-fixture/object", "silo fixture payload")
			Expect(request("GET", "/deps-fixture/object", "")).To(Equal("silo fixture payload"))
			Expect(instance.Restart(ctx)).To(Succeed())
			Expect(request("GET", "/deps-fixture/object", "")).To(Equal("silo fixture payload"))
			if kind == RuntimeDocker {
				Expect(instance.State.Tag).To(Equal(siloTag))
			}
		}, Entry("binary", RuntimeBinary), Entry("docker", RuntimeDocker))
	It("downloads and renders the pinned chart", func() {
		_, spec, _ := config.GetService("silo")
		svc := testContext(*spec, Options{WaitTimeout: 3 * time.Minute})
		svc.RunDir = GinkgoT().TempDir()
		chart, err := prepareHelmChart(context.Background(), svc)
		Expect(err).NotTo(HaveOccurred())
		helm, err := (&helmRuntime{}).ensureHelm(context.Background())
		Expect(err).NotTo(HaveOccurred())
		output, err := exec.Command(helm, "template", "deps-silo", chart, "--set", "mode=standalone", "--set", "persistence.enabled=false", "--set", "image.tag="+siloTag).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))
		Expect(string(output)).To(ContainSubstring("pgsty/silo:" + siloTag))
		Expect(strings.Count(string(output), "kind: Deployment")).To(Equal(1))
	})
})
