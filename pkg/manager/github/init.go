package github

import (
	"github.com/flanksource/deps/pkg/manager"
)

func init() {
	// Register GitHub managers with token sources that will be expanded via os.ExpandEnv
	manager.Register(NewGitHubReleaseManager("${GITHUB_TOKEN}", "${GH_TOKEN}", "${GITHUB_ACCESS_TOKEN}"))
	manager.Register(NewGitHubTagsManager("${GITHUB_TOKEN}", "${GH_TOKEN}", "${GITHUB_ACCESS_TOKEN}"))
}
