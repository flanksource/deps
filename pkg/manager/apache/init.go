package apache

import "github.com/flanksource/deps/pkg/manager"

func init() {
	// Register Apache archives manager
	manager.Register(NewApacheManager())
}