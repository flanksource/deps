package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/installer"
	"github.com/flanksource/deps/pkg/manager"
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
	infoCmd.Flags().IntVar(&iterateVersions, "iterate-versions", 0, "Number of releases to try when 'latest' has no matching assets (0=disabled)")
}

func runInfo(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	out := cmd.OutOrStdout()

	if infoAll {
		return runInfoAll(out, ctx, infoAllLatest)
	}

	if len(args) == 0 {
		return fmt.Errorf("package name required (or use --all to list all packages)")
	}

	toolSpec := installer.ParseTools(args)[0]
	inst := newCLIInstaller()
	preview, err := inst.Preview(toolSpec.Name, toolSpec.Version, &task.Task{})
	if err != nil {
		return err
	}

	pkg := preview.Package

	mgr, err := manager.GetGlobalRegistry().GetForPackage(pkg)
	if err != nil {
		return fmt.Errorf("failed to get manager for %s: %w", toolSpec.Name, err)
	}

	fmt.Fprintf(out, "Package: %s\n", pkg.Name)
	fmt.Fprintf(out, "Manager: %s\n", pkg.Manager)
	if preview.Plugin != "" {
		fmt.Fprintf(out, "Plugin: %s\n", preview.Plugin)
	}
	if pkg.Repo != "" {
		fmt.Fprintf(out, "Repo: %s\n", pkg.Repo)
	}
	if pkg.BinaryName != "" {
		fmt.Fprintf(out, "Binary: %s\n", pkg.BinaryName)
	}

	var versions []types.Version
	versions, err = mgr.DiscoverVersions(ctx, pkg, preview.Platform, infoVersionLimit)
	if err != nil {
		fmt.Fprintf(out, "\nVersions: (error: %v)\n", err)
	} else if len(versions) == 0 {
		fmt.Fprintf(out, "\nVersions: (none found)\n")
	} else {
		displayVersions := versions
		versionHeader := "Available Versions"
		if toolSpec.Version != "" && toolSpec.Version != "latest" && toolSpec.Version != "any" && toolSpec.Version != "stable" {
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

		fmt.Fprintf(out, "\n%s (showing %d):\n", versionHeader, len(displayVersions))

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
			if v.Tag != "" && v.Tag != v.Version && !strings.HasPrefix(v.Tag, "v") && len(v.Tag) == 8 {
				suffixes = append(suffixes, "build: "+v.Tag)
			}

			suffix := ""
			if len(suffixes) > 0 {
				suffix = " (" + strings.Join(suffixes, ", ") + ")"
			}
			if !v.Published.IsZero() {
				fmt.Fprintf(out, "  %s  %s%s\n", v.Version, v.Published.Format("2006-01-02"), suffix)
			} else {
				fmt.Fprintf(out, "  %s%s\n", v.Version, suffix)
			}
		}
	}

	printInstallPreview(out, preview)

	return nil
}

func runInfoAll(out io.Writer, ctx context.Context, includePrerelease bool) error {
	depsConfig := GetDepsConfig()
	plat := platform.Current()
	if osOverride != "" {
		plat.OS = osOverride
	}
	if archOverride != "" {
		plat.Arch = archOverride
	}

	var names []string
	for name := range depsConfig.Registry {
		names = append(names, name)
	}
	sort.Strings(names)

	maxNameLen := 7 // minimum "PACKAGE"
	for _, name := range names {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
	}

	fmt.Fprintf(out, "%-*s  %-12s  %-20s  %s\n", maxNameLen, "PACKAGE", "MANAGER", "VERSION", "STATUS")
	fmt.Fprintf(out, "%s  %s  %s  %s\n", strings.Repeat("-", maxNameLen), strings.Repeat("-", 12), strings.Repeat("-", 20), strings.Repeat("-", 20))

	constraint := "stable"
	if includePrerelease {
		constraint = "latest"
	}

	for _, name := range names {
		pkg := depsConfig.Registry[name]

		mgr, err := manager.GetGlobalRegistry().GetForPackage(pkg)
		if err != nil {
			fmt.Fprintf(out, "%-*s  %-12s  %-20s  %s\n", maxNameLen, name, pkg.Manager, "-", fmt.Sprintf("error: %v", err))
			continue
		}

		resolver := versionpkg.NewResolver(mgr)
		resolved, err := resolver.ResolveConstraint(ctx, pkg, constraint, plat)
		if err != nil {
			fmt.Fprintf(out, "%-*s  %-12s  %-20s  %s\n", maxNameLen, name, pkg.Manager, "-", fmt.Sprintf("error: %v", truncateError(err)))
			continue
		}

		fmt.Fprintf(out, "%-*s  %-12s  %-20s  %s\n", maxNameLen, name, pkg.Manager, truncateVersion(resolved, 20), "ok")
	}

	return nil
}

func printInstallPreview(out io.Writer, preview *installer.InstallPreview) {
	fmt.Fprintf(out, "\nInstall Preview for %s/%s:\n", preview.Platform.OS, preview.Platform.Arch)
	requested := preview.RequestedVersion
	if preview.RequestedInput != "" {
		requested = preview.RequestedInput
	}
	fmt.Fprintf(out, "  Requested: %s\n", requested)
	if preview.RequestedVersion != "" && preview.RequestedVersion != requested {
		fmt.Fprintf(out, "  Effective Request: %s\n", preview.RequestedVersion)
	}
	if preview.ResolvedVersion != "" {
		fmt.Fprintf(out, "  Resolved Version: %s\n", preview.ResolvedVersion)
	}
	if preview.EffectiveVersion != "" && preview.EffectiveVersion != preview.ResolvedVersion {
		fmt.Fprintf(out, "  Effective Version: %s\n", preview.EffectiveVersion)
	}
	if preview.AlreadyInstalled {
		fmt.Fprintf(out, "  Status: already installed\n")
		if preview.ExistingVersion != "" {
			fmt.Fprintf(out, "  Installed Version: %s\n", preview.ExistingVersion)
		}
		if preview.ExistingPath != "" {
			fmt.Fprintf(out, "  Existing Path: %s\n", preview.ExistingPath)
		}
		if preview.ExistingSource != "" {
			fmt.Fprintf(out, "  Existing Source: %s\n", preview.ExistingSource)
		}
		return
	}
	if preview.Plugin != "" {
		fmt.Fprintf(out, "  Method: plugin (%s)\n", preview.Plugin)
		return
	}
	if preview.Resolution == nil {
		fmt.Fprintf(out, "  Status: unresolved\n")
		return
	}

	fmt.Fprintf(out, "  Method: %s\n", preview.InstallMethod())
	if preview.Resolution.DownloadURL != "" {
		fmt.Fprintf(out, "  URL: %s\n", preview.Resolution.DownloadURL)
	}
	if preview.Resolution.Checksum != "" {
		fmt.Fprintf(out, "  Checksum: %s\n", preview.Resolution.Checksum)
	}
	if preview.Resolution.ChecksumURL != "" {
		fmt.Fprintf(out, "  Checksum URL: %s\n", preview.Resolution.ChecksumURL)
	}
	if preview.Resolution.IsArchive {
		fmt.Fprintf(out, "  Archive: yes\n")
		if preview.Resolution.BinaryPath != "" {
			fmt.Fprintf(out, "  Binary Path: %s\n", preview.Resolution.BinaryPath)
		}
	}
	if preview.Resolution.GitHubAsset != nil {
		fmt.Fprintf(out, "  Asset: %s@%s\n", preview.Resolution.GitHubAsset.Repo, preview.Resolution.GitHubAsset.Tag)
	}
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
