package maven

import "github.com/flanksource/deps/pkg/manager"

func init() {
	// Register Maven manager
	manager.Register(NewMavenManager())
}
