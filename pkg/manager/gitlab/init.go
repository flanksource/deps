package gitlab

import (
	"os"

	"github.com/flanksource/deps/pkg/manager"
)

func init() {
	// Register GitLab manager with token from multiple possible environment variables
	token, tokenSource := detectGitLabToken()
	manager.Register(NewGitLabReleaseManager(token, tokenSource))
}

// detectGitLabToken checks multiple environment variables for GitLab tokens
func detectGitLabToken() (token, source string) {
	// Check in order of preference
	tokenSources := []string{
		"GITLAB_TOKEN",
		"GL_TOKEN",
		"GITLAB_ACCESS_TOKEN",
	}

	for _, envVar := range tokenSources {
		if token := os.Getenv(envVar); token != "" {
			return token, envVar
		}
	}

	return "", ""
}
