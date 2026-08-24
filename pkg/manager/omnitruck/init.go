package omnitruck

import "github.com/flanksource/deps/pkg/manager"

func init() {
	// Register Omnitruck (Chef/CINC omnibus) manager
	manager.Register(New())
}
