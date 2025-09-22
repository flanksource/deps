package cmd

import (
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/types"
	"github.com/spf13/cobra"
)

// DependencyInfo represents information about a single dependency
type DependencyInfo struct {
	Name      string `json:"name" pretty:"label=Dependency"`
	Platforms string `json:"platforms" pretty:"label=Platforms"`
	Checksum  string `json:"checksum" pretty:"label=Checksum"`
	Source    string `json:"source" pretty:"label=Source"`
}

// DependencyList represents a list of dependencies for table display
type DependencyList struct {
	Dependencies []DependencyInfo `json:"dependencies" pretty:"table"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available dependencies",
	Long:  `List all available dependencies that can be installed.`,
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

// extractPlatforms converts asset patterns to a comma-separated list of supported platforms
func extractPlatforms(pkg types.Package) string {
	platforms := make(map[string]bool)

	// Extract from asset patterns
	for pattern := range pkg.AssetPatterns {
		// Handle patterns like "linux-*", "darwin-*,windows-*"
		if strings.Contains(pattern, "*") {
			// Extract base patterns
			parts := strings.Split(pattern, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasSuffix(part, "-*") {
					// Add common architectures for wildcard patterns
					base := strings.TrimSuffix(part, "-*")
					if base == "linux" || base == "darwin" || base == "windows" {
						platforms[base+"-amd64"] = true
						platforms[base+"-arm64"] = true
					}
				} else {
					platforms[part] = true
				}
			}
		} else {
			platforms[strings.TrimSpace(pattern)] = true
		}
	}

	// Handle special managers
	if len(platforms) == 0 {
		switch pkg.Manager {
		case "maven":
			platforms["java"] = true
		case "direct":
			platforms["direct-url"] = true
		default:
			platforms["unknown"] = true
		}
	}

	// Convert to sorted slice
	var platformList []string
	for platform := range platforms {
		platformList = append(platformList, platform)
	}
	sort.Strings(platformList)

	if len(platformList) == 0 {
		return "unknown"
	}
	return strings.Join(platformList, ", ")
}

// hasChecksum determines if a dependency has checksum verification configured
func hasChecksum(pkg types.Package) string {
	if pkg.ChecksumFile != "" || pkg.ChecksumExpr != "" {
		return "Yes"
	}
	return "No"
}

// getSource determines the source of a dependency (simplified for now)
func getSource(name string) string {
	// For now, we'll assume all dependencies come from the registry
	// In the future, this could check deps.yaml and deps-lock.yaml
	return "registry"
}

func runList(cmd *cobra.Command, args []string) error {
	// Get all dependencies from the merged registry
	registry := config.GetGlobalRegistry()

	// Collect dependency information
	var dependencies []DependencyInfo
	for name, pkg := range registry.Registry {
		dependencies = append(dependencies, DependencyInfo{
			Name:      name,
			Platforms: extractPlatforms(pkg),
			Checksum:  hasChecksum(pkg),
			Source:    getSource(name),
		})
	}

	// Sort alphabetically by name
	sort.Slice(dependencies, func(i, j int) bool {
		return dependencies[i].Name < dependencies[j].Name
	})

	// Create dependency list structure
	dependencyList := DependencyList{
		Dependencies: dependencies,
	}

	// Format and display using clicky
	result, err := clicky.Format(dependencyList)
	if err != nil {
		return err
	}

	cmd.Println(result)
	return nil
}
