package manager

import (
	"regexp"
	"sort"
	"strings"

	"github.com/flanksource/deps/pkg/platform"
)

// compressedExtensions are archive formats deps knows how to extract.
var compressedExtensions = []string{
	".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz",
	".zip", ".7z", ".rar",
}

// versionToken matches a version-like name segment such as "1.4.2" or "v2.0.0-rc1".
var versionToken = regexp.MustCompile(`^v?\d[\dA-Za-z.+]*`)

const assetSeparators = "-_."

// Score weights for ranking release assets. Canonical naming dominates, then platform
// specificity, so that a package's own binary always outranks a sibling package's.
const (
	scoreCanonicalName = 100
	scorePlatformToken = 40
	scoreCompressed    = 20
	scoreExactPlatform = 10
)

// AssetSelection describes what a caller is looking for when ranking release assets.
type AssetSelection struct {
	Platform    platform.Platform
	PackageName string
	// IncludeNonBinary keeps checksum, signature and documentation files in the
	// candidate set. Only set it when the caller's pattern explicitly asks for one.
	IncludeNonBinary bool
}

// IsCompressedAsset reports whether the filename is an archive deps can extract.
func IsCompressedAsset(filename string) bool {
	lower := strings.ToLower(filename)
	for _, ext := range compressedExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// PatternTargetsNonBinary reports whether an asset pattern explicitly asks for a
// checksum, signature or documentation file rather than a binary.
func PatternTargetsNonBinary(pattern string) bool {
	lower := strings.ToLower(strings.TrimSpace(pattern))
	for _, ext := range nonBinaryExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// SelectBestAsset ranks candidate assets and returns the best match for the selection,
// or nil when nothing survives. Candidates are usually the assets matching a caller's
// glob; checksum and signature files are dropped first so that an orphan
// "tool-linux-amd64.sha256" can never be installed in place of the binary it describes.
// Ranking is order-independent: ties break on the shortest, then lowest, name.
func SelectBestAsset(candidates []AssetInfo, sel AssetSelection) *AssetInfo {
	if !sel.IncludeNonBinary {
		candidates = filterNonBinaryFiles(candidates)
	}
	if len(candidates) == 0 {
		return nil
	}

	ranked := make([]AssetInfo, len(candidates))
	copy(ranked, candidates)
	scores := make(map[string]int, len(ranked))
	for _, asset := range ranked {
		scores[asset.Name] = scoreAsset(asset.Name, sel)
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i].Name, ranked[j].Name
		if scores[a] != scores[b] {
			return scores[a] > scores[b]
		}
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		return a < b
	})

	return &ranked[0]
}

// AssetScores returns each candidate's score keyed by name, for diagnostics.
func AssetScores(candidates []AssetInfo, sel AssetSelection) map[string]int {
	scores := make(map[string]int, len(candidates))
	for _, asset := range candidates {
		scores[asset.Name] = scoreAsset(asset.Name, sel)
	}
	return scores
}

func scoreAsset(name string, sel AssetSelection) int {
	base := stripAssetExtensions(name)
	lowerBase := strings.ToLower(base)
	osAliases := getOSAliases(sel.Platform.OS)
	archAliases := getArchAliases(sel.Platform.Arch)

	score := 0
	if IsCompressedAsset(name) {
		score += scoreCompressed
	}
	if sel.Platform.OS != "" && sel.Platform.Arch != "" &&
		strings.Contains(lowerBase, strings.ToLower(sel.Platform.OS)) &&
		strings.Contains(lowerBase, strings.ToLower(sel.Platform.Arch)) {
		score += scoreExactPlatform
	}

	if sel.PackageName == "" {
		return score
	}

	lowerName := strings.ToLower(sel.PackageName)
	if !strings.HasPrefix(lowerBase, lowerName) {
		return score
	}

	remainder := strings.TrimLeft(lowerBase[len(lowerName):], assetSeparators)
	if consumeKnownToken(remainder, osAliases, archAliases) != "" || remainder == "" {
		score += scorePlatformToken
	}
	if isCanonicalRemainder(remainder, osAliases, archAliases) {
		score += scoreCanonicalName
	}

	return score
}

// isCanonicalRemainder reports whether everything after the package name consists
// solely of platform tokens and version segments, e.g. "linux-amd64" or
// "1.4.2-linux-amd64" but not "start-linux-amd64".
func isCanonicalRemainder(remainder string, osAliases, archAliases []string) bool {
	for remainder != "" {
		token := consumeKnownToken(remainder, osAliases, archAliases)
		if token == "" {
			return false
		}
		remainder = strings.TrimLeft(remainder[len(token):], assetSeparators)
	}
	return true
}

// consumeKnownToken returns the leading OS alias, architecture alias or version
// segment of s, or "" when s starts with something else. The longest alias wins so
// that "x86_64" is not consumed as "x86".
func consumeKnownToken(s string, osAliases, archAliases []string) string {
	longest := ""
	for _, alias := range append(append([]string{}, osAliases...), archAliases...) {
		alias = strings.ToLower(alias)
		if len(alias) <= len(longest) || !strings.HasPrefix(s, alias) {
			continue
		}
		if len(s) == len(alias) || strings.ContainsRune(assetSeparators, rune(s[len(alias)])) {
			longest = alias
		}
	}
	if longest != "" {
		return longest
	}

	if match := versionToken.FindString(s); match != "" {
		if len(s) == len(match) || strings.ContainsRune(assetSeparators, rune(s[len(match)])) {
			return match
		}
	}
	return ""
}

// stripAssetExtensions removes archive and executable extensions so that
// "deps-linux-amd64.tar.gz" and "deps-windows-amd64.exe" both reduce to their
// canonical "{name}-{os}-{arch}" form.
func stripAssetExtensions(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range compressedExtensions {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	if strings.HasSuffix(lower, ".exe") {
		return name[:len(name)-len(".exe")]
	}
	return name
}
