package start

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/flanksource/deps/pkg/types"
)

func validateServiceOptions(spec types.ServiceSpec, options Options) error {
	for name, value := range options.Ports {
		found := false
		for _, port := range spec.Ports {
			if port.Name == name {
				found = true
			}
		}
		if !found || value < 1 || value > 65535 {
			return fmt.Errorf("invalid port override %s=%d", name, value)
		}
	}
	primary, _ := spec.PrimaryPort()
	if port, ok := options.Ports[primary.Name]; ok && options.Port != 0 && port != options.Port {
		return fmt.Errorf("conflicting primary port overrides")
	}
	enabled, _ := strconv.ParseBool(options.Parameters["tls"])
	if !enabled {
		return nil
	}
	dir := options.Parameters["tls-dir"]
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("tls-dir must be an absolute certificate directory")
	}
	for _, name := range []string{"ca.crt", "server.crt", "server.key", "server.pem", "client.crt", "client.key", "client.pem"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("TLS certificate %s: %w", name, err)
		}
	}
	return nil
}
