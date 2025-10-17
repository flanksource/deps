package url

import "github.com/flanksource/deps/pkg/manager"

func init() {
	// Register URL manager
	manager.Register(NewURLManager())
}
