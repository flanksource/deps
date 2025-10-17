package github

import (
	"github.com/flanksource/deps/pkg/manager"
)

func init() {
	// Initialize singleton client with default token sources
	_ = GetClient()

	// Register GitHub managers (they use the shared singleton client)
	manager.Register(NewGitHubReleaseManager())
	manager.Register(NewGitHubTagsManager())
	manager.Register(NewGitHubBuildManager())
}
