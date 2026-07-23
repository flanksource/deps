package installer

import (
	"fmt"
	"strings"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

type installIdentity struct {
	Name      string
	AssetName string
}

func deriveAssetInstallName(assetName, version string, plat platform.Platform) (string, error) {
	baseName := stripAssetExtension(assetName)
	metadata := []string{
		strings.TrimPrefix(version, "v"),
		"v" + strings.TrimPrefix(version, "v"),
		plat.OS,
		plat.Arch,
		"darwin", "macos", "linux", "windows", "win",
		"amd64", "x86_64", "x64", "arm64", "aarch64",
	}
	cut := len(baseName)
	for _, value := range metadata {
		if index := metadataIndex(baseName, value); index >= 0 && index < cut {
			cut = index
		}
	}
	name := strings.TrimRight(baseName[:cut], "._-")
	if name == "" {
		return "", fmt.Errorf("cannot derive binary name from GitHub asset %q", assetName)
	}
	return name, nil
}

func (i *Installer) resolveInstallIdentity(name, version string, resolution *types.Resolution) (installIdentity, error) {
	identity := installIdentity{Name: name}
	if resolution.GitHubAsset != nil {
		identity.AssetName = resolution.GitHubAsset.AssetName
	}
	if len(i.options.AssetFilters) == 0 {
		return identity, nil
	}
	if identity.AssetName == "" {
		return installIdentity{}, fmt.Errorf("--asset selected a resolution without GitHub asset metadata")
	}
	derivedName, err := deriveAssetInstallName(identity.AssetName, version, resolution.Platform)
	if err != nil {
		return installIdentity{}, err
	}
	if resolution.Platform.IsWindows() && strings.HasSuffix(strings.ToLower(identity.AssetName), ".exe") {
		derivedName += ".exe"
	}
	identity.Name = derivedName
	return identity, nil
}

func stripAssetExtension(assetName string) string {
	lower := strings.ToLower(assetName)
	for _, extension := range []string{
		".tar.gz", ".tar.bz2", ".tar.xz", ".tbz2", ".tgz", ".txz",
		".zip", ".7z", ".rar", ".exe", ".pkg", ".msi",
	} {
		if strings.HasSuffix(lower, extension) {
			return assetName[:len(assetName)-len(extension)]
		}
	}
	return assetName
}

func metadataIndex(assetName, metadata string) int {
	if metadata == "" || metadata == "v" {
		return -1
	}
	lowerName := strings.ToLower(assetName)
	lowerMetadata := strings.ToLower(metadata)
	for offset := 0; offset < len(lowerName); {
		index := strings.Index(lowerName[offset:], lowerMetadata)
		if index < 0 {
			return -1
		}
		index += offset
		end := index + len(lowerMetadata)
		if index > 0 && isAssetSeparator(lowerName[index-1]) && (end == len(lowerName) || isAssetSeparator(lowerName[end])) {
			return index - 1
		}
		offset = index + 1
	}
	return -1
}

func isAssetSeparator(char byte) bool {
	return char == '_' || char == '-' || char == '.'
}
