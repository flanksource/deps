package version

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
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
