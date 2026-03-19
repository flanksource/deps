package version

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/flanksource/deps/pkg/types"
)

func TestClickHouseVersionExpr(t *testing.T) {
	expr := `(tag.contains("-stable") || tag.contains("-lts")) ? (tag.substring(1).split("-")[0].split(".").size() >= 4 ? tag.substring(1).split("-")[0].split(".")[0] + "." + tag.substring(1).split("-")[0].split(".")[1] + "." + tag.substring(1).split("-")[0].split(".")[2] + "+" + tag.substring(1).split("-")[0].split(".")[3] : tag.substring(1).split("-")[0]) : ""`

	tests := []struct {
		tag         string
		wantVersion string
		wantInclude bool
	}{
		{"v26.2.4.23-stable", "26.2.4+23", true},
		{"v25.8.18.1-lts", "25.8.18+1", true},
		{"v26.2.3.2-stable", "26.2.3+2", true},
		{"v26.2.1.1139-stable", "26.2.1+1139", true},
		{"v19.3.7-stable", "19.3.7", true},
		{"v26.2.4.23-testing", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			versions := []types.Version{{
				Tag:     tt.tag,
				Version: Normalize(tt.tag),
			}}

			result, err := ApplyVersionExpr(versions, expr)
			if err != nil {
				t.Fatalf("ApplyVersionExpr error: %v", err)
			}

			if tt.wantInclude {
				if len(result) == 0 {
					t.Fatalf("expected version to be included, but was filtered out")
				}
				if result[0].Version != tt.wantVersion {
					t.Errorf("Version = %q, want %q", result[0].Version, tt.wantVersion)
				}
				// Verify it's valid semver
				_, err := semver.NewVersion(result[0].Version)
				if err != nil {
					t.Errorf("version %q is not valid semver: %v", result[0].Version, err)
				}
			} else {
				if len(result) != 0 {
					t.Errorf("expected version to be excluded, but got %v", result)
				}
			}
		})
	}
}

func TestClickHousePartialVersionConstraint(t *testing.T) {
	expr := `(tag.contains("-stable") || tag.contains("-lts")) ? (tag.substring(1).split("-")[0].split(".").size() >= 4 ? tag.substring(1).split("-")[0].split(".")[0] + "." + tag.substring(1).split("-")[0].split(".")[1] + "." + tag.substring(1).split("-")[0].split(".")[2] + "+" + tag.substring(1).split("-")[0].split(".")[3] : tag.substring(1).split("-")[0]) : ""`

	tags := []string{
		"v26.2.4.23-stable",
		"v26.2.3.2-stable",
		"v25.8.18.1-lts",
		"v26.1.4.35-stable",
		"v25.12.8.9-stable",
		"v25.3.14.14-lts",
	}

	versions := make([]types.Version, len(tags))
	for i, tag := range tags {
		versions[i] = types.Version{Tag: tag, Version: Normalize(tag)}
	}

	filtered, err := ApplyVersionExpr(versions, expr)
	if err != nil {
		t.Fatalf("ApplyVersionExpr error: %v", err)
	}

	// All should be valid semver now
	for _, v := range filtered {
		if _, err := semver.NewVersion(v.Version); err != nil {
			t.Errorf("version %q (from tag %s) is not valid semver: %v", v.Version, v.Tag, err)
		}
	}

	// Partial constraint "25" should match only v25.x tags
	constraint, err := ParseConstraint("25")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}

	var matched []string
	for _, v := range filtered {
		if constraint.Check(v.Version) {
			matched = append(matched, v.Version)
		}
	}

	if len(matched) != 3 {
		t.Fatalf("expected 3 versions matching '25', got %d: %v", len(matched), matched)
	}

	// Partial constraint "26" should match only v26.x tags
	constraint26, err := ParseConstraint("26")
	if err != nil {
		t.Fatalf("ParseConstraint error: %v", err)
	}

	var matched26 []string
	for _, v := range filtered {
		if constraint26.Check(v.Version) {
			matched26 = append(matched26, v.Version)
		}
	}

	if len(matched26) != 3 {
		t.Fatalf("expected 3 versions matching '26', got %d: %v", len(matched26), matched26)
	}
}
