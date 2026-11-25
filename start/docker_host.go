package start

import (
	"context"
	"fmt"
	"net/url"
)

// dockerEndpointHost returns the host that ports published by a daemon at
// endpoint are reachable on: the endpoint's hostname for tcp, http, https and
// ssh daemons (the remote docker context case), and "localhost" for local
// unix sockets and named pipes. Any other endpoint is an error rather than a
// guess, because a wrong host yields a connection that silently points at the
// wrong machine.
func dockerEndpointHost(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid docker endpoint %q: %w", endpoint, err)
	}
	switch u.Scheme {
	case "unix", "npipe":
		return "localhost", nil
	case "tcp", "http", "https", "ssh":
		if u.Hostname() == "" {
			return "", fmt.Errorf("docker endpoint %q has no host", endpoint)
		}
		return u.Hostname(), nil
	default:
		return "", fmt.Errorf("cannot derive a service host from docker endpoint %q: unsupported scheme %q", endpoint, u.Scheme)
	}
}

// daemonHost resolves the host services are reachable on from the active
// docker endpoint (DOCKER_HOST or the current docker context). Local sockets
// defer to the sdk, which also handles running inside a container.
func (r *dockerRuntime) daemonHost(ctx context.Context) (string, error) {
	cli, err := r.client()
	if err != nil {
		return "", err
	}
	endpoint := cli.DaemonHost()
	host, err := dockerEndpointHost(endpoint)
	if err != nil {
		return "", err
	}
	if host != "localhost" {
		return host, nil
	}
	return cli.DaemonHostWithContext(ctx)
}
