package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/deps/pkg/installer"
	"github.com/flanksource/deps/pkg/manager"
	ghmanager "github.com/flanksource/deps/pkg/manager/github"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	versionpkg "github.com/flanksource/deps/pkg/version"
	"github.com/spf13/cobra"
)

var (
	infoVersionLimit int
	infoAll          bool
	infoAllLatest    bool
)

var infoCmd = &cobra.Command{
	Use:   "info [package[@version]]",
	Short: "Show detailed information about a package",
	Long: `Display available versions, resolved URLs, and package metadata.

Examples:
  deps info kubectl           # Show kubectl info with latest versions
  deps info jq@1.7            # Show jq info resolved to version 1.7
  deps info helm --versions 20 # Show 20 versions
  deps info --all             # Show all packages with resolved stable versions
  deps info --all --all-latest # Show all packages with latest versions (including prereleases)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInfo,
}

func init() {
	rootCmd.AddCommand(infoCmd)
	infoCmd.Flags().IntVar(&infoVersionLimit, "versions", 100, "Number of versions to display")
	infoCmd.Flags().BoolVar(&infoAll, "all", false, "Show all packages in the registry with resolved versions")
	infoCmd.Flags().BoolVar(&infoAllLatest, "all-latest", false, "With --all, include prereleases when resolving latest version")
}

func runInfo(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if infoAll {
		return runInfoAll(ctx, infoAllLatest)
	}

	if len(args) == 0 {
		return fmt.Errorf("package name required (or use --all to list all packages)")
	}

	// Parse package@version
	toolSpec := installer.ParseTools(args)[0]

	// Get package from registry
	depsConfig := GetDepsConfig()
	pkg, exists := depsConfig.Registry[toolSpec.Name]
	if !exists {
		return fmt.Errorf("package %s not found in registry", toolSpec.Name)
	}

	// Get manager
	mgr, err := manager.GetGlobalRegistry().GetForPackage(pkg)
	if err != nil {
		return fmt.Errorf("failed to get manager for %s: %w", toolSpec.Name, err)
	}

	// Get current platform
	plat := platform.Current()

	// Print package info header
	fmt.Printf("Package: %s\n", toolSpec.Name)
	fmt.Printf("Manager: %s\n", pkg.Manager)
	if pkg.Repo != "" {
		fmt.Printf("Repo: %s\n", pkg.Repo)
	}
	if pkg.BinaryName != "" {
		fmt.Printf("Binary: %s\n", pkg.BinaryName)
	}

	// Discover versions - use REST API for GitHub releases to get published dates
	var versions []types.Version
	if ghMgr, ok := mgr.(*ghmanager.GitHubReleaseManager); ok {
		versions, err = ghMgr.DiscoverVersionsViaREST(ctx, pkg, infoVersionLimit)
	} else {
		versions, err = mgr.DiscoverVersions(ctx, pkg, plat, infoVersionLimit)
	}
	if err != nil {
		fmt.Printf("\nVersions: (error: %v)\n", err)
	} else if len(versions) == 0 {
		fmt.Printf("\nVersions: (none found)\n")
	} else {
		// Filter versions if a constraint is provided
		displayVersions := versions
		versionHeader := "Available Versions"
		if toolSpec.Version != "" && toolSpec.Version != "latest" && toolSpec.Version != "any" && toolSpec.Version != "stable" {
			// Try to parse as a constraint and filter matching versions
			if constraint, parseErr := versionpkg.ParseConstraint(toolSpec.Version); parseErr == nil {
				var filtered []types.Version
				for _, v := range versions {
					if constraint.Check(v.Version) {
						filtered = append(filtered, v)
					}
				}
				if len(filtered) > 0 {
					displayVersions = filtered
					versionHeader = fmt.Sprintf("Matching Versions for %s", toolSpec.Version)
				}
			}
		}

		fmt.Printf("\n%s (showing %d):\n", versionHeader, len(displayVersions))

		// Find first stable version for marking
		firstStableIdx := -1
		for i, v := range displayVersions {
			if !v.Prerelease {
				firstStableIdx = i
				break
			}
		}

		for i, v := range displayVersions {
			var suffixes []string
			if i == firstStableIdx {
				suffixes = append(suffixes, "latest stable")
			}
			if v.Prerelease {
				suffixes = append(suffixes, "prerelease")
			}
			// Show build tag if it's a build date (github_build manager uses YYYYMMDD format)
			if v.Tag != "" && v.Tag != v.Version && !strings.HasPrefix(v.Tag, "v") && len(v.Tag) == 8 {
				suffixes = append(suffixes, "build: "+v.Tag)
			}

			suffix := ""
			if len(suffixes) > 0 {
				suffix = " (" + strings.Join(suffixes, ", ") + ")"
			}
			if !v.Published.IsZero() {
				fmt.Printf("  %s  %s%s\n", v.Version, v.Published.Format("2006-01-02"), suffix)
			} else {
				fmt.Printf("  %s%s\n", v.Version, suffix)
			}
		}
	}

	// Resolve version constraint using the same resolver as install
	resolveVersion := toolSpec.Version
	if resolveVersion == "" {
		resolveVersion = "latest"
	}

	// Use centralized version resolver (same as install command)
	resolver := versionpkg.NewResolver(mgr)
	resolved, err := resolver.ResolveConstraint(ctx, pkg, resolveVersion, plat)
	if err != nil {
		fmt.Printf("\nResolved for %s/%s:\n", plat.OS, plat.Arch)
		fmt.Printf("  Version: %s\n", resolveVersion)
		fmt.Printf("  Error: %v\n", err)
		return nil
	}

	versionLabel := resolved
	if resolveVersion != resolved {
		versionLabel = fmt.Sprintf("%s (resolved to %s)", resolveVersion, resolved)
	}
	resolveVersion = resolved

	// Get resolution details
	fmt.Printf("\nResolved for %s/%s:\n", plat.OS, plat.Arch)
	fmt.Printf("  Version: %s\n", versionLabel)

	resolution, err := mgr.Resolve(ctx, pkg, resolveVersion, plat)
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
		return nil
	}

	fmt.Printf("  URL: %s\n", resolution.DownloadURL)
	if resolution.Checksum != "" {
		fmt.Printf("  Checksum: %s\n", resolution.Checksum)
	}
	if resolution.ChecksumURL != "" {
		fmt.Printf("  Checksum URL: %s\n", resolution.ChecksumURL)
	}
	if resolution.IsArchive {
		fmt.Printf("  Archive: yes\n")
		if resolution.BinaryPath != "" {
			fmt.Printf("  Binary Path: %s\n", resolution.BinaryPath)
		}
	}

	return nil
}

func runInfoAll(ctx context.Context, includePrerelease bool) error {
	depsConfig := GetDepsConfig()
	plat := platform.Current()

	// Get sorted package names
	var names []string
	for name := range depsConfig.Registry {
		names = append(names, name)
	}
	sort.Strings(names)

	// Calculate column widths
	maxNameLen := 7 // minimum "PACKAGE"
	for _, name := range names {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
	}

	// Print header
	fmt.Printf("%-*s  %-12s  %-20s  %s\n", maxNameLen, "PACKAGE", "MANAGER", "VERSION", "STATUS")
	fmt.Printf("%s  %s  %s  %s\n", strings.Repeat("-", maxNameLen), strings.Repeat("-", 12), strings.Repeat("-", 20), strings.Repeat("-", 20))

	// Use "latest" to include prereleases, "stable" for stable only
	constraint := "stable"
	if includePrerelease {
		constraint = "latest"
	}

	for _, name := range names {
		pkg := depsConfig.Registry[name]

		mgr, err := manager.GetGlobalRegistry().GetForPackage(pkg)
		if err != nil {
			fmt.Printf("%-*s  %-12s  %-20s  %s\n", maxNameLen, name, pkg.Manager, "-", fmt.Sprintf("error: %v", err))
			continue
		}

		resolver := versionpkg.NewResolver(mgr)
		resolved, err := resolver.ResolveConstraint(ctx, pkg, constraint, plat)
		if err != nil {
			fmt.Printf("%-*s  %-12s  %-20s  %s\n", maxNameLen, name, pkg.Manager, "-", fmt.Sprintf("error: %v", truncateError(err)))
			continue
		}

		fmt.Printf("%-*s  %-12s  %-20s  %s\n", maxNameLen, name, pkg.Manager, truncateVersion(resolved, 20), "ok")
	}

	return nil
}

func truncateVersion(version string, maxLen int) string {
	if len(version) <= maxLen {
		return version
	}
	return version[:maxLen-3] + "..."
}

func truncateError(err error) string {
	msg := err.Error()
	// Get first line only
	if idx := strings.Index(msg, "\n"); idx != -1 {
		msg = msg[:idx]
	}
	if len(msg) > 50 {
		return msg[:47] + "..."
	}
	return msg
}
