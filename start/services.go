package start

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ServiceRequest names one service for StartServices along with the options
// it is started with.
type ServiceRequest struct {
	Name    string
	Options []Option
}

type (
	startFunc func(ctx context.Context, name string, opts ...Option) (*Instance, error)
	stopFunc  func(ctx context.Context, name string, opts ...Option) error
)

// StartServices starts every requested service concurrently via Start and
// waits for all of them. The returned map is keyed by service name. Service
// state is stored per name, so requesting the same name twice is an error
// rather than a silent last-writer-wins.
//
// On failure the error joins every failed start (each prefixed with the
// service name) and the returned map still holds the instances that did
// start. The caller owns those and must release them with StopServices.
func StartServices(ctx context.Context, requests []ServiceRequest) (map[string]*Instance, error) {
	return startServices(ctx, requests, Start)
}

func startServices(ctx context.Context, requests []ServiceRequest, start startFunc) (map[string]*Instance, error) {
	seen := make(map[string]bool, len(requests))
	for _, r := range requests {
		if seen[r.Name] {
			return nil, fmt.Errorf("service %q requested more than once", r.Name)
		}
		seen[r.Name] = true
	}

	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		instances = make(map[string]*Instance, len(requests))
		errs      []error
	)
	for _, r := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			instance, err := start(ctx, r.Name, r.Options...)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("start %s: %w", r.Name, err))
				return
			}
			instances[r.Name] = instance
		}()
	}
	wg.Wait()
	return instances, errors.Join(errs...)
}

// StopServices stops every instance concurrently (typically the map returned
// by StartServices, including the partial map from a failed start) and joins
// the failures, each prefixed with the service name.
func StopServices(ctx context.Context, instances map[string]*Instance) error {
	return stopServices(ctx, instances, Stop)
}

func stopServices(ctx context.Context, instances map[string]*Instance, stop stopFunc) error {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		errs []error
	)
	for name, instance := range instances {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := stop(ctx, name, WithStateDir(instance.opts.StateDir)); err != nil {
				mu.Lock()
				defer mu.Unlock()
				errs = append(errs, fmt.Errorf("stop %s: %w", name, err))
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}
