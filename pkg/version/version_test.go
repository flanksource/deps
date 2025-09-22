package version

import (
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"v1.2.3", "1.2.3"},
		{"V1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"release-1.2.3", "1.2.3"},
		{"Release-1.2.3", "1.2.3"},
		{"version-1.2.3", "1.2.3"},
		{"Version-1.2.3", "1.2.3"},
		{"1.2.3-release", "1.2.3"},
		{"1.2.3-Release", "1.2.3"},
		{"v1.2.3-alpha", "1.2.3-alpha"},
		{"", ""},
		{" v1.2.3 ", "1.2.3"},
	}

	for _, test := range tests {
		result := Normalize(test.input)
		if result != test.expected {
			t.Errorf("Normalize(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

func TestExtractFromOutput(t *testing.T) {
	tests := []struct {
		output   string
		pattern  string
		expected string
		hasError bool
	}{
		{"kubectl version v1.28.2", "", "1.28.2", false},
		{"version: 3.5.0", "", "3.5.0", false},
		{"jq-1.6", "", "1.6", false},
		{"Client Version: v1.28.2", `Client Version:\s*v?(\d+\.\d+\.\d+)`, "1.28.2", false},
		{"no version found", "", "", true},
		{"version 1.2.3", `invalid[regex`, "", true},
	}

	for _, test := range tests {
		result, err := ExtractFromOutput(test.output, test.pattern)
		if test.hasError {
			if err == nil {
				t.Errorf("ExtractFromOutput(%q, %q) expected error, got nil", test.output, test.pattern)
			}
		} else {
			if err != nil {
				t.Errorf("ExtractFromOutput(%q, %q) unexpected error: %v", test.output, test.pattern, err)
			} else if result != test.expected {
				t.Errorf("ExtractFromOutput(%q, %q) = %q, expected %q", test.output, test.pattern, result, test.expected)
			}
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"1.2.3", "v1.2.3", 0},
		{"v3.5.0", "3.5.0", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.3.0", "1.2.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
	}

	for _, test := range tests {
		result, err := Compare(test.v1, test.v2)
		if err != nil {
			t.Errorf("Compare(%q, %q) unexpected error: %v", test.v1, test.v2, err)
		} else if result != test.expected {
			t.Errorf("Compare(%q, %q) = %d, expected %d", test.v1, test.v2, result, test.expected)
		}
	}
}

func TestIsCompatible(t *testing.T) {
	tests := []struct {
		installed string
		required  string
		expected  bool
	}{
		{"v3.5.0", "3.4.0", true}, // Same major, installed >= required
		{"3.5.0", "v3.5.0", true}, // Same version, different prefixes
		{"3.4.0", "3.5.0", false}, // Same major, installed < required
		{"4.0.0", "3.5.0", false}, // Different major
		{"2.9.0", "3.0.0", false}, // Different major
	}

	for _, test := range tests {
		result, err := IsCompatible(test.installed, test.required)
		if err != nil {
			t.Errorf("IsCompatible(%q, %q) unexpected error: %v", test.installed, test.required, err)
		} else if result != test.expected {
			t.Errorf("IsCompatible(%q, %q) = %t, expected %t", test.installed, test.required, result, test.expected)
		}
	}
}

func TestSatisfiesConstraint(t *testing.T) {
	tests := []struct {
		version    string
		constraint string
		expected   bool
	}{
		{"v1.2.3", "latest", true},
		{"1.2.3", "1.2.3", true},
		{"v1.2.3", "1.2.3", true},
		{"1.2.4", "^1.2.0", true},
		{"1.3.0", "~1.2.0", false},
		{"2.0.0", "^1.2.0", false},
		{"1.2.3", ">=1.2.0", true},
	}

	for _, test := range tests {
		result, err := SatisfiesConstraint(test.version, test.constraint)
		if err != nil {
			t.Errorf("SatisfiesConstraint(%q, %q) unexpected error: %v", test.version, test.constraint, err)
		} else if result != test.expected {
			t.Errorf("SatisfiesConstraint(%q, %q) = %t, expected %t", test.version, test.constraint, result, test.expected)
		}
	}
}
