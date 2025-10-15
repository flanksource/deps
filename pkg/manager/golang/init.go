package golang

import "github.com/flanksource/deps/pkg/manager"

func init() {
	manager.Register(NewGoManager())
}
