package builtin

import (
	// Import for side effects only - registers built-in plugins
	_ "github.com/flanksource/deps/pkg/plugin"
)

// init registers all built-in plugins
func init() {
	// Built-in plugins have been replaced with pipeline-based post-processing
	// See Package.PostProcess field for complex package handling
}
