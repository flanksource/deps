package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/manager/github"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show authentication status for package managers",
	Long:  `whoami displays authentication status and user information for configured package managers like GitHub.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWhoAmI()
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}

func runWhoAmI() error {
	ctx := context.Background()

	fmt.Println("Package Manager Authentication Status:")
	fmt.Println("=====================================")

	// Check GitHub authentication
	if err := checkGitHubAuth(ctx); err != nil {
		return fmt.Errorf("failed to check GitHub authentication: %w", err)
	}

	return nil
}

func checkGitHubAuth(ctx context.Context) error {
	// Get the global registry and look for GitHub manager
	registry := manager.GetGlobalRegistry()

	// Try to get the GitHub manager
	mgr, exists := registry.Get("github_release")
	if !exists {
		fmt.Println("\n❌ GitHub Manager: Not available")
		return nil
	}

	// Cast to GitHub manager to access WhoAmI method
	githubMgr, ok := mgr.(*github.GitHubReleaseManager)
	if !ok {
		fmt.Println("\n❌ GitHub Manager: Invalid type")
		return nil
	}

	// Get authentication status
	status := githubMgr.WhoAmI(ctx)

	fmt.Printf("\n🔧 GitHub Release Manager:\n")

	// Show token source
	if status.TokenSource != "" {
		fmt.Printf("  Token Source: %s\n", status.TokenSource)
	} else {
		fmt.Printf("  Token Source: None (checked GITHUB_TOKEN, GH_TOKEN, GITHUB_ACCESS_TOKEN)\n")
	}

	// Show authentication status
	if status.Authenticated {
		fmt.Printf("  Authenticated: ✅ Yes\n")
	} else {
		fmt.Printf("  Authenticated: ❌ No\n")
		if status.Error != "" {
			fmt.Printf("  Error: %s\n", status.Error)
		}
	}

	// Show user information if authenticated
	if status.User != nil {
		fmt.Printf("\n👤 User Information:\n")
		fmt.Printf("  Username: %s\n", status.User.Username)

		if status.User.Name != "" {
			fmt.Printf("  Name: %s\n", status.User.Name)
		}

		if status.User.Email != "" {
			fmt.Printf("  Email: %s\n", status.User.Email)
		}

		if status.User.Company != "" {
			fmt.Printf("  Company: %s\n", status.User.Company)
		}

		if status.User.CreatedAt != nil {
			fmt.Printf("  Account Created: %s\n", status.User.CreatedAt.Format("2006-01-02"))
		}
	}

	// Show rate limit information
	if status.RateLimit != nil {
		fmt.Printf("\n📊 API Rate Limits:\n")
		fmt.Printf("  Remaining: %d/%d\n", status.RateLimit.Remaining, status.RateLimit.Total)

		if status.RateLimit.ResetTime != nil {
			timeUntilReset := time.Until(*status.RateLimit.ResetTime)
			fmt.Printf("  Resets in: %s\n", formatRateLimitDuration(timeUntilReset))
		}

		// Warn if rate limit is low
		if status.RateLimit.Remaining < 100 {
			fmt.Printf("  ⚠️  Warning: Low rate limit remaining\n")
		}
	}

	// Show permissions status
	if status.Authenticated {
		if status.HasPermissions {
			fmt.Printf("\n✅ Token has sufficient permissions for GitHub releases\n")
		} else {
			fmt.Printf("\n❌ Token lacks required permissions\n")
		}
	}

	// Provide helpful tips
	if !status.Authenticated {
		fmt.Printf("\n💡 Tips:\n")
		fmt.Printf("  - Set GITHUB_TOKEN environment variable for authenticated access\n")
		fmt.Printf("  - Use 'gh auth token' if you have GitHub CLI installed\n")
		fmt.Printf("  - Create a personal access token at https://github.com/settings/tokens\n")
		fmt.Printf("  - No special scopes required for public repository access\n")
	}

	return nil
}

// formatRateLimitDuration formats a duration in a human-readable way for rate limits
func formatRateLimitDuration(d time.Duration) string {
	if d < 0 {
		return "expired"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}
