package start

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"

	"github.com/docker/docker/client"
)

// newDockerClient honors DOCKER_HOST, then the docker CLI's current context
// (which client.FromEnv alone ignores), then the default socket.
func newDockerClient() (*client.Client, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if os.Getenv("DOCKER_HOST") == "" {
		if host := hostFromDockerContext(); host != "" {
			opts = append(opts, client.WithHost(host))
		}
	}
	return client.NewClientWithOpts(opts...)
}

// daemonHost returns the hostname services on this daemon are reachable at:
// "localhost" for unix/npipe sockets, the endpoint hostname for tcp daemons.
func daemonHost(c *client.Client) string {
	u, err := url.Parse(c.DaemonHost())
	if err != nil || u.Scheme == "unix" || u.Scheme == "npipe" || u.Hostname() == "" {
		return "localhost"
	}
	return u.Hostname()
}

// hostFromDockerContext resolves the docker endpoint of the CLI's current
// context from ~/.docker, returning "" for the default context or on any
// parse failure (falling back to the SDK default).
func hostFromDockerContext() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	name := os.Getenv("DOCKER_CONTEXT")
	if name == "" {
		var config struct {
			CurrentContext string `json:"currentContext"`
		}
		data, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
		if err != nil || json.Unmarshal(data, &config) != nil {
			return ""
		}
		name = config.CurrentContext
	}
	if name == "" || name == "default" {
		return ""
	}

	sum := sha256.Sum256([]byte(name))
	metaPath := filepath.Join(home, ".docker", "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")
	var meta struct {
		Endpoints struct {
			Docker struct {
				Host string `json:"Host"`
			} `json:"docker"`
		} `json:"Endpoints"`
	}
	data, err := os.ReadFile(metaPath)
	if err != nil || json.Unmarshal(data, &meta) != nil {
		return ""
	}
	return meta.Endpoints.Docker.Host
}
