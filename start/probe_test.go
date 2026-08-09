package start

import (
	"context"
	"net"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/deps/pkg/types"
)

// listenerPort opens a loopback listener and returns its port, so probes run
// against a real socket rather than a mocked dialer.
func listenerPort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(listener.Close)
	return listener.Addr().(*net.TCPAddr).Port
}

// freePort returns a port nothing is listening on.
func freePort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())
	port := listener.Addr().(*net.TCPAddr).Port
	Expect(listener.Close()).To(Succeed())
	return port
}

func serviceOn(port int, health *types.HealthCheck) *ServiceContext {
	return &ServiceContext{
		Name: "probe-service",
		Spec: types.ServiceSpec{
			Ports:  []types.ServicePort{{Name: "api", Port: port, Primary: true}},
			Health: health,
		},
		Opts: Options{WaitTimeout: 300 * time.Millisecond},
	}
}

var _ = Describe("readiness probes", func() {
	Describe("stages", func() {
		It("waits on the primary port when the spec declares no health check", func() {
			svc := serviceOn(5432, nil)

			stages, err := probeStages(context.Background(), svc, types.HealthCheck{}, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(stageDescriptions(stages)).To(Equal([]string{"tcp localhost:5432"}))
		})

		It("gates a stdout_match check on the primary port accepting", func() {
			svc := serviceOn(5432, nil)
			watch := &processWatch{output: func() string { return "" }}

			stages, err := probeStages(context.Background(), svc, types.HealthCheck{StdoutMatch: "ready to accept"}, watch)
			Expect(err).ToNot(HaveOccurred())
			Expect(stageDescriptions(stages)).To(Equal([]string{
				`"ready to accept" in process output`,
				"tcp localhost:5432",
			}))
		})

		It("gates an exec check on the primary port accepting", func() {
			svc := serviceOn(5432, nil)
			watch := &processWatch{execProbe: func(context.Context, []string) bool { return true }}

			stages, err := probeStages(context.Background(), svc, types.HealthCheck{Exec: []string{"pg_isready"}}, watch)
			Expect(err).ToNot(HaveOccurred())
			Expect(stageDescriptions(stages)).To(Equal([]string{
				"exec pg_isready",
				"tcp localhost:5432",
			}))
		})

		It("gates an http check on the primary port accepting", func() {
			svc := serviceOn(9200, nil)

			stages, err := probeStages(context.Background(), svc, types.HealthCheck{HTTP: "/_cluster/health"}, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(stageDescriptions(stages)).To(Equal([]string{
				"http://localhost:9200/_cluster/health",
				"tcp localhost:9200",
			}))
		})

		It("does not repeat the port gate when the spec check is already that port", func() {
			svc := serviceOn(6379, nil)

			stages, err := probeStages(context.Background(), svc, types.HealthCheck{Port: "api"}, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(stageDescriptions(stages)).To(HaveLen(1))
		})

		It("rejects a health check naming a port the service does not declare", func() {
			svc := serviceOn(5432, nil)

			_, err := probeStages(context.Background(), svc, types.HealthCheck{Port: "metrics"}, nil)
			Expect(err).To(MatchError(ContainSubstring(`unknown port "metrics"`)))
		})

		It("targets the published port of a secondary named port", func() {
			svc := serviceOn(9200, nil)
			svc.Spec.Ports = append(svc.Spec.Ports, types.ServicePort{Name: "transport", Port: 9300})
			svc.Opts.Port = 19200

			port, err := probePort(svc, types.HealthCheck{Port: "transport"})
			Expect(err).ToNot(HaveOccurred())
			Expect(port).To(Equal(9300))

			primary, err := probePort(svc, types.HealthCheck{})
			Expect(err).ToNot(HaveOccurred())
			Expect(primary).To(Equal(19200), "the primary port honours the --port override")
		})
	})

	Describe("awaitHealthy", func() {
		It("returns once the primary port accepts", func() {
			svc := serviceOn(listenerPort(), nil)

			Expect(awaitHealthy(context.Background(), svc, nil, nil)).To(Succeed())
		})

		It("times out on the port gate when stdout_match passed but nothing is listening", func() {
			port := freePort()
			svc := serviceOn(port, nil)
			watch := &processWatch{
				alive:  func() bool { return true },
				output: func() string { return "database system is ready to accept connections" },
			}

			err := awaitHealthy(context.Background(), svc, &types.HealthCheck{
				StdoutMatch: "ready to accept connections",
				Interval:    "10ms",
			}, watch)
			Expect(err).To(MatchError(ContainSubstring("tcp localhost:" + strconv.Itoa(port))))
		})

		It("fails fast when the process exits before becoming ready", func() {
			svc := serviceOn(freePort(), nil)
			watch := &processWatch{
				alive:  func() bool { return false },
				output: func() string { return "FATAL: could not bind" },
			}

			err := awaitHealthy(context.Background(), svc, &types.HealthCheck{Interval: "10ms"}, watch)
			Expect(err).To(MatchError(ContainSubstring("FATAL: could not bind")))
		})

		It("reports every unmet condition it waits on", func() {
			var waited []string
			svc := serviceOn(freePort(), nil)
			svc.Opts.OnWaiting = func(r Readiness) { waited = append(waited, r.Waiting) }

			Expect(awaitHealthy(context.Background(), svc, &types.HealthCheck{Interval: "10ms"}, nil)).ToNot(Succeed())
			Expect(waited).ToNot(BeEmpty())
			Expect(waited[0]).To(HavePrefix("tcp localhost:"))
		})

		It("reports the ports a runtime detected while waiting", func() {
			var reported []int
			svc := serviceOn(freePort(), nil)
			svc.Opts.OnWaiting = func(r Readiness) { reported = r.Ports }
			watch := &processWatch{
				alive: func() bool { return true },
				ports: func() []int { return []int{5432} },
			}

			Expect(awaitHealthy(context.Background(), svc, &types.HealthCheck{Interval: "10ms"}, watch)).ToNot(Succeed())
			Expect(reported).To(Equal([]int{5432}))
		})
	})
})

func stageDescriptions(stages []probeStage) []string {
	descriptions := make([]string, len(stages))
	for i, stage := range stages {
		descriptions[i] = stage.desc
	}
	return descriptions
}
