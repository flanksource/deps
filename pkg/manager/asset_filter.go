package manager

import (
	"fmt"
	"strings"
)

// AssetInfo represents information about a downloadable asset
type AssetInfo struct {
	Name        string
	DownloadURL string
	SHA256      string
}

// FilterAssetsByPlatform applies iterative filtering to narrow down assets for a specific platform.
// It performs three stages:
// 1. Filter out non-binary files (signatures, checksums, docs)
// 2. Filter by OS with aliases (mac/macos/darwin, win/windows, linux)
// 3. Filter by architecture with aliases (x86_64/amd64/i386/i686/x86/386 -> x64, aarch64 -> arm64)
func FilterAssetsByPlatform(assets []AssetInfo, os, arch string) ([]AssetInfo, error) {
	if len(assets) == 0 {
		return nil, fmt.Errorf("no assets provided for filtering")
	}

	// Stage 1: Filter non-binary files
	filtered := filterNonBinaryFiles(assets)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no assets found after filtering non-binary files (signatures, checksums, docs)")
	}

	// Stage 2: Filter by OS
	filtered, err := filterByOS(filtered, os)
	if err != nil {
		return nil, err
	}

	// Stage 3: Filter by architecture
	filtered, err = filterByArch(filtered, arch)
	if err != nil {
		return nil, err
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("no assets found matching OS '%s' and architecture '%s'", os, arch)
	}

	return filtered, nil
}

// filterNonBinaryFiles removes signature files, checksums, and documentation
func filterNonBinaryFiles(assets []AssetInfo) []AssetInfo {
	nonBinaryExtensions := []string{
		".asc", ".sig", ".gpg", ".pem", // Signature files
		".sha1", ".sha256", ".sha512", // Checksum files
		".md5", ".checksum", // More checksum files
		".txt", ".json", ".yaml", // Text files (often checksums)
	}

	nonBinaryPrefixes := []string{
		"CHANGELOG", "README", "LICENSE",
		"COPYING", "NOTICE", "AUTHORS",
	}

	var filtered []AssetInfo
	for _, asset := range assets {
		name := asset.Name
		nameUpper := strings.ToUpper(name)

		// Check extensions
		isNonBinary := false
		for _, ext := range nonBinaryExtensions {
			if strings.HasSuffix(strings.ToLower(name), ext) {
				isNonBinary = true
				break
			}
		}

		// Check prefixes for documentation files
		if !isNonBinary {
			for _, prefix := range nonBinaryPrefixes {
				if strings.HasPrefix(nameUpper, prefix) {
					isNonBinary = true
					break
				}
			}
		}

		if !isNonBinary {
			filtered = append(filtered, asset)
		}
	}

	return filtered
}

// getOSAliases returns all aliases for a given OS
func getOSAliases(os string) []string {
	aliases := map[string][]string{
		"darwin":  {"darwin", "mac", "macos", "osx"},
		"windows": {"windows", "win", "win32", "win64"},
		"linux":   {"linux"},
	}

	if list, ok := aliases[os]; ok {
		return list
	}
	return []string{os}
}

// filterByOS filters assets by OS using aliases
func filterByOS(assets []AssetInfo, os string) ([]AssetInfo, error) {
	if len(assets) == 0 {
		return assets, nil
	}

	aliases := getOSAliases(os)
	var filtered []AssetInfo

	for _, asset := range assets {
		nameLower := strings.ToLower(asset.Name)
		for _, alias := range aliases {
			if strings.Contains(nameLower, strings.ToLower(alias)) {
				filtered = append(filtered, asset)
				break
			}
		}
	}

	// If no OS-specific files found, return all (might be universal binaries)
	if len(filtered) == 0 {
		return assets, nil
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("no assets found matching OS '%s' (tried: %s)",
			os, strings.Join(aliases, ", "))
	}

	return filtered, nil
}

// getArchAliases returns all aliases for a given architecture
func getArchAliases(arch string) []string {
	aliases := map[string][]string{
		"amd64": {"amd64", "x86_64", "x64", "x86-64", "i386", "i686", "x86", "386", "64bit", "64-bit"},
		"arm64": {"arm64", "aarch64", "arm"},
		"arm":   {"arm", "armv7", "armv7l"},
	}

	// Normalize arch input
	normalizedArch := arch
	for canonical, aliasList := range aliases {
		for _, alias := range aliasList {
			if arch == alias {
				normalizedArch = canonical
				break
			}
		}
	}

	if list, ok := aliases[normalizedArch]; ok {
		return list
	}
	return []string{arch}
}

// filterByArch filters assets by architecture using aliases
func filterByArch(assets []AssetInfo, arch string) ([]AssetInfo, error) {
	if len(assets) == 0 {
		return assets, nil
	}

	aliases := getArchAliases(arch)
	var filtered []AssetInfo

	for _, asset := range assets {
		nameLower := strings.ToLower(asset.Name)
		for _, alias := range aliases {
			if strings.Contains(nameLower, strings.ToLower(alias)) {
				filtered = append(filtered, asset)
				break
			}
		}
	}

	// If no arch-specific files found, return all (might be universal binaries)
	if len(filtered) == 0 {
		return assets, nil
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("no assets found matching architecture '%s' (tried: %s)",
			arch, strings.Join(aliases, ", "))
	}

	return filtered, nil
}
