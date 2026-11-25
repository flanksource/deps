package start

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StartServices", func() {
	var (
		ctx     = context.Background()
		failure = errors.New("boom")
	)

	// starter fails the named services and records peak concurrency.
	type starter struct {
		fail           map[string]error
		inFlight, peak atomic.Int32
		mu             sync.Mutex
		started        []string
	}
	start := func(s *starter) startFunc {
		return func(_ context.Context, name string, _ ...Option) (*Instance, error) {
			now := s.inFlight.Add(1)
			defer s.inFlight.Add(-1)
			for peak := s.peak.Load(); now > peak && !s.peak.CompareAndSwap(peak, now); peak = s.peak.Load() {
			}
			time.Sleep(50 * time.Millisecond)
			if err := s.fail[name]; err != nil {
				return nil, err
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			s.started = append(s.started, name)
			return &Instance{Name: name}, nil
		}
	}

	It("starts every service concurrently and keys instances by name", func() {
		s := &starter{}
		requests := []ServiceRequest{{Name: "ministack"}, {Name: "nats"}, {Name: "fake-gcs-server"}}

		instances, err := startServices(ctx, requests, start(s))

		Expect(err).ToNot(HaveOccurred())
		Expect(instances).To(HaveLen(3))
		Expect(instances).To(HaveKey("nats"))
		Expect(s.peak.Load()).To(BeNumerically(">", 1))
	})

	It("joins every failure with its service name and still returns the instances that started", func() {
		s := &starter{fail: map[string]error{"nats": failure, "rclone": failure}}
		requests := []ServiceRequest{{Name: "ministack"}, {Name: "nats"}, {Name: "rclone"}}

		instances, err := startServices(ctx, requests, start(s))

		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, failure)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring(`start nats: boom`))
		Expect(err.Error()).To(ContainSubstring(`start rclone: boom`))
		Expect(instances).To(HaveLen(1))
		Expect(instances).To(HaveKey("ministack"))
	})

	It("rejects a service requested twice because service state is keyed by name", func() {
		_, err := startServices(ctx, []ServiceRequest{{Name: "nats"}, {Name: "nats"}}, start(&starter{}))
		Expect(err).To(MatchError(ContainSubstring(`service "nats" requested more than once`)))
	})

	It("reports an unknown service from the real Start", func() {
		instances, err := StartServices(ctx, []ServiceRequest{{Name: "no-such-service", Options: []Option{WithStateDir(GinkgoT().TempDir())}}})
		Expect(err).To(MatchError(ContainSubstring(`start no-such-service: unknown service "no-such-service"`)))
		Expect(instances).To(BeEmpty())
	})

	It("stops every instance and joins failures", func() {
		var stopped atomic.Int32
		stop := func(_ context.Context, name string, _ ...Option) error {
			stopped.Add(1)
			if name == "nats" {
				return failure
			}
			return nil
		}
		err := stopServices(ctx, map[string]*Instance{"ministack": {Name: "ministack"}, "nats": {Name: "nats"}}, stop)

		Expect(stopped.Load()).To(Equal(int32(2)))
		Expect(err).To(MatchError(ContainSubstring("stop nats: boom")))
	})
})
