// Package omnitruck resolves packages published through an Omnitruck server —
// the release API Progress Chef and the CINC project both serve their omnibus
// packages from.
//
// It is a manager rather than a `url` package definition because Omnitruck
// answers in its own vocabulary: a version list at one endpoint, and a separate
// metadata endpoint that returns the download URL and its checksum together,
// keyed by a platform triple (`p`, `pv`, `m`) that is not the OS/arch pair the
// rest of deps speaks. The `url` manager can express neither half — its
// `AssetsExpr` is only wired into the github manager, and a `URLTemplate` would
// have to hardcode a distribution path and give up checksum verification.
//
// Everything it serves is an operating-system package (`.deb`, `.rpm`, `.dmg`),
// never a relocatable archive: omnibus builds link absolute paths under
// /opt/<project> into the interpreter, its shared libraries and its load path.
// Resolutions from here are therefore installed system-wide.
package omnitruck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/flanksource/commons/logger"
	depshttp "github.com/flanksource/deps/pkg/http"
	"github.com/flanksource/deps/pkg/manager"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"github.com/flanksource/deps/pkg/version"
)

// DefaultBaseURL is the CINC project's Omnitruck. Chef's own server speaks the
// same API and is selected with the `base_url` extra.
const DefaultBaseURL = "https://omnitruck.cinc.sh"

// DefaultChannel is the release channel served when none is configured.
const DefaultChannel = "stable"

// Manager implements manager.PackageManager against an Omnitruck server.
type Manager struct {
	client *http.Client
}

// New builds an Omnitruck manager.
func New() *Manager {
	return &Manager{client: depshttp.GetHttpClient()}
}

// Name returns the manager identifier.
func (m *Manager) Name() string { return "omnitruck" }

// settings is the per-package Omnitruck configuration, read from Extra.
type settings struct {
	BaseURL string
	Channel string
	Product string
}

// settingsFor reads the Omnitruck coordinates from a package. Product defaults
// to the package name, which is how every package published this way is keyed.
func settingsFor(pkg types.Package) settings {
	resolved := settings{BaseURL: DefaultBaseURL, Channel: DefaultChannel, Product: pkg.Name}
	if value, ok := extra(pkg, "base_url"); ok {
		resolved.BaseURL = value
	}
	if value, ok := extra(pkg, "channel"); ok {
		resolved.Channel = value
	}
	if value, ok := extra(pkg, "product"); ok {
		resolved.Product = value
	}
	resolved.BaseURL = strings.TrimSuffix(resolved.BaseURL, "/")
	return resolved
}

func extra(pkg types.Package, key string) (string, bool) {
	value, exists := pkg.Extra[key]
	if !exists {
		return "", false
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", false
	}
	return text, true
}

// target is the platform triple Omnitruck indexes its artifacts by.
type target struct {
	// Platform is Omnitruck's `p` — a distribution family, not an OS.
	Platform string
	// PlatformVersion is `pv`, the distribution release.
	PlatformVersion string
	// Machine is `m`, the processor architecture in uname's vocabulary.
	Machine string
}

// targets maps a deps platform onto Omnitruck's triple.
//
// Linux resolves to the Ubuntu build for every distribution rather than
// matching the host: an omnibus package carries its own interpreter and every
// library it needs under /opt, so the distribution in the artifact name selects
// a packaging format, not a set of runtime dependencies. Picking one keeps this
// from needing to know what the host distribution is.
var targets = map[string]target{
	"linux-amd64":  {Platform: "ubuntu", PlatformVersion: "22.04", Machine: "x86_64"},
	"linux-arm64":  {Platform: "ubuntu", PlatformVersion: "22.04", Machine: "aarch64"},
	"darwin-amd64": {Platform: "mac_os_x", PlatformVersion: "14", Machine: "x86_64"},
	"darwin-arm64": {Platform: "mac_os_x", PlatformVersion: "14", Machine: "arm64"},
}

func targetFor(plat platform.Platform) (target, error) {
	resolved, ok := targets[plat.String()]
	if !ok {
		return target{}, fmt.Errorf(
			"omnitruck publishes no packages for %s: supported platforms are %s",
			plat.String(), strings.Join(supportedPlatforms(), ", "))
	}
	return resolved, nil
}

func supportedPlatforms() []string {
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	// Map iteration order is random and this text ends up in an error message.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// DiscoverVersions lists every version the channel carries.
func (m *Manager) DiscoverVersions(ctx context.Context, pkg types.Package, plat platform.Platform, limit int) ([]types.Version, error) {
	config := settingsFor(pkg)
	endpoint := fmt.Sprintf("%s/%s/%s/versions/all", config.BaseURL, config.Channel, config.Product)

	logger.Tracef("omnitruck: discovering versions for %s from %s", pkg.Name, endpoint)

	var raw []string
	if err := m.getJSON(ctx, endpoint, &raw); err != nil {
		return nil, fmt.Errorf("omnitruck: discover versions for %s: %w", pkg.Name, err)
	}

	versions := make([]types.Version, 0, len(raw))
	for _, entry := range raw {
		versions = append(versions, types.ParseVersion(version.Normalize(entry), entry))
	}

	if pkg.VersionExpr != "" {
		filtered, err := version.ApplyVersionExpr(versions, pkg.VersionExpr)
		if err != nil {
			return nil, fmt.Errorf("omnitruck: apply version_expr for %s: %w", pkg.Name, err)
		}
		versions = filtered
	}

	versions = version.FilterToValidSemver(versions)
	version.SortVersions(versions)

	if limit > 0 && len(versions) > limit {
		versions = versions[:limit]
	}
	return versions, nil
}

// metadata is Omnitruck's answer for one artifact.
type metadata struct {
	SHA1    string `json:"sha1"`
	SHA256  string `json:"sha256"`
	URL     string `json:"url"`
	Version string `json:"version"`
}

// Resolve asks Omnitruck for the artifact matching one version and platform.
//
// The response carries the checksum alongside the URL, so nothing here has to
// find and parse a separate checksum file — an artifact resolved this way is
// always verifiable.
func (m *Manager) Resolve(ctx context.Context, pkg types.Package, requested string, plat platform.Platform) (*types.Resolution, error) {
	config := settingsFor(pkg)
	platformTarget, err := targetFor(plat)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("p", platformTarget.Platform)
	query.Set("pv", platformTarget.PlatformVersion)
	query.Set("m", platformTarget.Machine)
	query.Set("v", omnitruckVersion(requested))

	endpoint := fmt.Sprintf("%s/%s/%s/metadata?%s",
		config.BaseURL, config.Channel, config.Product, query.Encode())

	logger.Tracef("omnitruck: resolving %s from %s", pkg.Name, endpoint)

	var resolved metadata
	if err := m.getJSON(ctx, endpoint, &resolved); err != nil {
		return nil, fmt.Errorf("omnitruck: resolve %s %s for %s: %w", pkg.Name, requested, plat.String(), err)
	}

	// An unknown version answers 200 with an empty body rather than 404, so an
	// absent URL is the only signal that the version does not exist.
	if resolved.URL == "" {
		return nil, &manager.ErrVersionNotFound{Package: pkg.Name, Version: requested}
	}
	if resolved.SHA256 == "" {
		return nil, fmt.Errorf(
			"omnitruck: %s %s has no sha256: refusing to install an artifact that cannot be verified",
			pkg.Name, resolved.Version)
	}

	return &types.Resolution{
		Package:     pkg,
		Version:     resolved.Version,
		Platform:    plat,
		DownloadURL: resolved.URL,
		Checksum:    "sha256:" + resolved.SHA256,
		// Never an archive. Omnibus packages are operating-system packages that
		// only work at the absolute prefix they were built for, so they are
		// handed to the system installer rather than unpacked into bin-dir.
		IsArchive: false,
	}, nil
}

// omnitruckVersion maps a deps version onto the `v` parameter. Omnitruck spells
// "newest" as "latest" and takes a bare version otherwise.
func omnitruckVersion(requested string) string {
	if requested == "" || requested == "latest" {
		return "latest"
	}
	return strings.TrimPrefix(requested, "v")
}

// Install is the shared pipeline's job, as it is for every other manager.
func (m *Manager) Install(ctx context.Context, resolution *types.Resolution, opts types.InstallOptions) error {
	return fmt.Errorf("omnitruck: install is handled by the shared installer pipeline")
}

// GetChecksums reports the checksum of every platform's artifact for a version.
func (m *Manager) GetChecksums(ctx context.Context, pkg types.Package, requested string) (map[string]string, error) {
	checksums := map[string]string{}
	for _, name := range supportedPlatforms() {
		parts := strings.SplitN(name, "-", 2)
		resolution, err := m.Resolve(ctx, pkg, requested, platform.Platform{OS: parts[0], Arch: parts[1]})
		if err != nil {
			// A platform the product does not build for is not an error for the
			// set: cinc-auditor ships no darwin-amd64 build for every version.
			logger.Tracef("omnitruck: no %s artifact for %s %s: %v", name, pkg.Name, requested, err)
			continue
		}
		checksums[name] = resolution.Checksum
	}
	if len(checksums) == 0 {
		return nil, fmt.Errorf("omnitruck: no artifacts found for %s %s", pkg.Name, requested)
	}
	return checksums, nil
}

// Verify is the shared version-check pipeline's job.
func (m *Manager) Verify(ctx context.Context, binaryPath string, pkg types.Package) (*types.InstalledInfo, error) {
	return nil, fmt.Errorf("omnitruck: verify is handled by the shared version check")
}

// getJSON fetches one Omnitruck endpoint. The Accept header is required: the
// same endpoints answer with tab-separated text by default.
func (m *Manager) getJSON(ctx context.Context, endpoint string, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := m.client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: HTTP %d", endpoint, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}
