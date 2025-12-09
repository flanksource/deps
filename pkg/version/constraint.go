package version

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/deps/pkg/types"
)

// Constraint represents a version constraint
type Constraint interface {
	Check(version string) bool
	String() string
}

// ParseConstraint parses a version constraint string
func ParseConstraint(constraint string) (Constraint, error) {
	constraint = strings.TrimSpace(constraint)

	switch constraint {
	case "", "*":
		return &AnyConstraint{}, nil
	case "latest", "stable":
		return &StableConstraint{}, nil
	default:
		// Check for partial version first
		if IsPartialVersion(constraint) {
			return &PartialVersionConstraint{pattern: Normalize(constraint)}, nil
		}

		// Try to parse as semver constraint
		c, err := semver.NewConstraint(constraint)
		if err != nil {
			return nil, fmt.Errorf("invalid version constraint %s: %w", constraint, err)
		}
		return &SemverConstraint{constraint: c, original: constraint}, nil
	}
}

// AnyConstraint accepts any version including pre-releases
type AnyConstraint struct{}

func (c *AnyConstraint) Check(version string) bool {
	return true
}

func (c *AnyConstraint) String() string {
	return "*"
}

// StableConstraint accepts any stable version (no pre-releases)
type StableConstraint struct{}

func (c *StableConstraint) Check(version string) bool {
	v, err := semver.NewVersion(Normalize(version))
	if err != nil {
		return false
	}
	return v.Prerelease() == ""
}

func (c *StableConstraint) String() string {
	return "stable"
}

// SemverConstraint uses semver constraint checking
type SemverConstraint struct {
	constraint *semver.Constraints
	original   string
}

func (c *SemverConstraint) Check(version string) bool {
	v, err := semver.NewVersion(Normalize(version))
	if err != nil {
		return false
	}
	return c.constraint.Check(v)
}

func (c *SemverConstraint) String() string {
	return c.original
}

// PartialVersionConstraint matches versions that start with a given prefix
// Examples: "1" matches "1.0.0", "1.5.2", etc. "1.5" matches "1.5.0", "1.5.3", etc.
type PartialVersionConstraint struct {
	pattern string
}

func (c *PartialVersionConstraint) Check(version string) bool {
	normalized := Normalize(version)

	// Parse both the pattern and version as semver
	patternSemver, err := c.parsePatternAsSemver()
	if err != nil {
		return false
	}

	versionSemver, err := semver.NewVersion(normalized)
	if err != nil {
		return false
	}

	// Check if version matches the pattern
	dotCount := strings.Count(c.pattern, ".")

	switch dotCount {
	case 0:
		// Major only: "2" should match "2.x.x"
		return versionSemver.Major() == patternSemver.Major()
	case 1:
		// Major.minor: "1.5" should match "1.5.x"
		return versionSemver.Major() == patternSemver.Major() &&
			versionSemver.Minor() == patternSemver.Minor()
	default:
		return false
	}
}

func (c *PartialVersionConstraint) String() string {
	return c.pattern
}

// parsePatternAsSemver converts the partial pattern to a full semver for comparison
func (c *PartialVersionConstraint) parsePatternAsSemver() (*semver.Version, error) {
	dotCount := strings.Count(c.pattern, ".")

	var fullVersion string
	switch dotCount {
	case 0:
		// Major only: "2" -> "2.0.0"
		fullVersion = c.pattern + ".0.0"
	case 1:
		// Major.minor: "1.5" -> "1.5.0"
		fullVersion = c.pattern + ".0"
	default:
		return nil, fmt.Errorf("invalid partial version pattern: %s", c.pattern)
	}

	return semver.NewVersion(fullVersion)
}

// FilterVersions filters a list of versions by the given constraint
func FilterVersions(versions []string, constraint Constraint) []string {
	if constraint == nil {
		return versions
	}

	var filtered []string
	for _, version := range versions {
		if constraint.Check(version) {
			filtered = append(filtered, version)
		}
	}
	return filtered
}

// IsPrerelease checks if a version is a pre-release
func IsPrerelease(version string) bool {
	v, err := semver.NewVersion(Normalize(version))
	if err != nil {
		// Try to detect pre-release patterns manually
		lower := strings.ToLower(version)
		prereleaseParts := []string{"alpha", "beta", "rc", "pre", "dev", "snapshot"}
		for _, part := range prereleaseParts {
			if strings.Contains(lower, part) {
				return true
			}
		}
		return false
	}
	return v.Prerelease() != ""
}

// GetLatestVersion returns the latest version from a list, optionally excluding pre-releases
func GetLatestVersion(versions []string, stableOnly bool) (string, error) {
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions available")
	}

	var candidates []string
	if stableOnly {
		for _, version := range versions {
			if !IsPrerelease(version) {
				candidates = append(candidates, version)
			}
		}
		if len(candidates) == 0 {
			return "", fmt.Errorf("no stable versions available")
		}
	} else {
		candidates = versions
	}

	// Sort versions and return the latest
	sorted, err := SortVersionsDescending(candidates)
	if err != nil {
		return "", fmt.Errorf("failed to sort versions: %w", err)
	}

	return sorted[0], nil
}

// SortVersionsDescending sorts versions in descending order (newest first)
func SortVersionsDescending(versions []string) ([]string, error) {
	if len(versions) == 0 {
		return versions, nil
	}

	// Convert to semver and sort
	var semvers []*semver.Version
	var mapping = make(map[string]string) // semver string -> original string

	for _, v := range versions {
		normalized := Normalize(v)
		sv, err := semver.NewVersion(normalized)
		if err != nil {
			// If we can't parse as semver, skip it
			continue
		}
		semvers = append(semvers, sv)
		mapping[sv.String()] = v
	}

	if len(semvers) == 0 {
		return nil, fmt.Errorf("no valid semantic versions found")
	}

	// Sort in descending order
	for i := 0; i < len(semvers)-1; i++ {
		for j := i + 1; j < len(semvers); j++ {
			if semvers[i].LessThan(semvers[j]) {
				semvers[i], semvers[j] = semvers[j], semvers[i]
			}
		}
	}

	// Convert back to original strings
	var result []string
	for _, sv := range semvers {
		result = append(result, mapping[sv.String()])
	}

	return result, nil
}

// IsValidSemanticVersion checks if a tag can be parsed as a valid semantic version
func IsValidSemanticVersion(tag string) bool {
	if tag == "" {
		return false
	}

	normalized := Normalize(tag)
	_, err := semver.NewVersion(normalized)
	return err == nil
}

// FilterValidVersions returns only tags that are valid semantic versions
func FilterValidVersions(tags []string) []string {
	var valid []string
	for _, tag := range tags {
		if IsValidSemanticVersion(tag) {
			valid = append(valid, tag)
		}
	}
	return valid
}

// ProcessTags converts raw tag names to Version structs with proper metadata
// Only includes tags that are valid semantic versions and sets prerelease status
func ProcessTags(tagNames []string) []types.Version {
	var versions []types.Version

	for _, tag := range tagNames {
		if !IsValidSemanticVersion(tag) {
			continue // Skip non-semantic versions
		}

		normalized := Normalize(tag)
		v, err := semver.NewVersion(normalized)
		if err != nil {
			continue // Should not happen since we validated above, but be safe
		}

		version := types.Version{
			Tag:        tag,
			Version:    normalized,
			Prerelease: len(v.Prerelease()) > 0,
		}

		versions = append(versions, version)
	}

	return versions
}

// SortVersionStructs sorts []types.Version in descending order (newest first)
func SortVersionStructs(versions []types.Version) []types.Version {
	if len(versions) <= 1 {
		return versions
	}

	// Create a copy to avoid modifying the original slice
	sorted := make([]types.Version, len(versions))
	copy(sorted, versions)

	// Sort versions in descending order (newest first)
	sort.Slice(sorted, func(i, j int) bool {
		v1, err1 := semver.NewVersion(sorted[i].Version)
		v2, err2 := semver.NewVersion(sorted[j].Version)

		if err1 != nil || err2 != nil {
			// Fallback to string comparison
			return sorted[i].Version > sorted[j].Version
		}

		return v1.GreaterThan(v2)
	})

	return sorted
}

// FilterAndSortVersions combines filtering and sorting in one operation
// Returns only valid semantic versions, optionally filtering out prereleases
func FilterAndSortVersions(tagNames []string, stableOnly bool) []types.Version {
	versions := ProcessTags(tagNames)

	if stableOnly {
		var stable []types.Version
		for _, v := range versions {
			if !v.Prerelease {
				stable = append(stable, v)
			}
		}
		versions = stable
	}

	return SortVersionStructs(versions)
}
