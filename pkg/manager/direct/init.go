package direct

import "github.com/flanksource/deps/pkg/manager"

func init() {
	// Register Direct URL manager
	manager.Register(NewDirectURLManager())
}
