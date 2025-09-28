package github

import (
	"os"

	"github.com/flanksource/deps/pkg/manager"
)

func init() {
	// Register GitHub managers with token from multiple possible environment variables
	token, tokenSource := detectGitHubToken()
	manager.Register(NewGitHubReleaseManager(token, tokenSource))
	manager.Register(NewGitHubTagsManager(token, tokenSource))
}

// detectGitHubToken checks multiple environment variables for GitHub tokens
func detectGitHubToken() (token, source string) {
	// Check in order of preference
	tokenSources := []string{
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"GITHUB_ACCESS_TOKEN",
	}

	for _, envVar := range tokenSources {
		if token := os.Getenv(envVar); token != "" {
			return token, envVar
		}
	}

	return "", ""
}
