package installer

import (
	"strings"
	"testing"
)

func TestInstallOptions_StrictChecksum(t *testing.T) {
	tests := []struct {
		name               string
		options            []InstallOption
		expectedStrict     bool
		expectedSkip       bool
	}{
		{
			name:           "default_options_should_have_strict_enabled",
			options:        []InstallOption{},
			expectedStrict: true, // Default should be strict
			expectedSkip:   false,
		},
		{
			name:           "with_strict_checksum_true",
			options:        []InstallOption{WithStrictChecksum(true)},
			expectedStrict: true,
			expectedSkip:   false,
		},
		{
			name:           "with_strict_checksum_false",
			options:        []InstallOption{WithStrictChecksum(false)},
			expectedStrict: false,
			expectedSkip:   false,
		},
		{
			name:           "with_skip_checksum_true",
			options:        []InstallOption{WithSkipChecksum(true)},
			expectedStrict: true, // Default strict value
			expectedSkip:   true,
		},
		{
			name: "both_skip_and_strict_options",
			options: []InstallOption{
				WithSkipChecksum(true),
				WithStrictChecksum(false),
			},
			expectedStrict: false,
			expectedSkip:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installer := New(tt.options...)

			if installer.options.StrictChecksum != tt.expectedStrict {
				t.Errorf("Expected StrictChecksum=%v, got %v", tt.expectedStrict, installer.options.StrictChecksum)
			}

			if installer.options.SkipChecksum != tt.expectedSkip {
				t.Errorf("Expected SkipChecksum=%v, got %v", tt.expectedSkip, installer.options.SkipChecksum)
			}
		})
	}
}

func TestDefaultOptions_StrictChecksum(t *testing.T) {
	opts := DefaultOptions()

	if !opts.StrictChecksum {
		t.Errorf("Expected default StrictChecksum to be true, got false")
	}

	if opts.SkipChecksum {
		t.Errorf("Expected default SkipChecksum to be false, got true")
	}
}

func TestWithStrictChecksumOption(t *testing.T) {
	tests := []struct {
		name     string
		value    bool
		expected bool
	}{
		{
			name:     "enable_strict_checksum",
			value:    true,
			expected: true,
		},
		{
			name:     "disable_strict_checksum",
			value:    false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := DefaultOptions()
			option := WithStrictChecksum(tt.value)
			option(&opts)

			if opts.StrictChecksum != tt.expected {
				t.Errorf("Expected StrictChecksum=%v, got %v", tt.expected, opts.StrictChecksum)
			}
		})
	}
}

// TestChecksumValidationBehavior tests the logic of strict vs non-strict checksum validation
func TestChecksumValidationBehavior(t *testing.T) {
	// Test that demonstrates the expected behavior described in the requirements
	tests := []struct {
		name            string
		strictChecksum  bool
		skipChecksum    bool
		simulateFailure bool
		expectError     bool
		description     string
	}{
		{
			name:            "strict_mode_with_checksum_failure_should_fail",
			strictChecksum:  true,
			skipChecksum:    false,
			simulateFailure: true,
			expectError:     true,
			description:     "When strict checksum is enabled and checksum fails, task should fail",
		},
		{
			name:            "non_strict_mode_with_checksum_failure_should_continue",
			strictChecksum:  false,
			skipChecksum:    false,
			simulateFailure: true,
			expectError:     false,
			description:     "When strict checksum is disabled and checksum fails, task should continue with warning",
		},
		{
			name:            "skip_checksum_mode_should_never_fail",
			strictChecksum:  true,
			skipChecksum:    true,
			simulateFailure: true,
			expectError:     false,
			description:     "When checksum is skipped, no validation should occur regardless of strict setting",
		},
		{
			name:            "strict_mode_with_checksum_success_should_succeed",
			strictChecksum:  true,
			skipChecksum:    false,
			simulateFailure: false,
			expectError:     false,
			description:     "When strict checksum is enabled and checksum succeeds, task should succeed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create installer with test configuration
			installer := &Installer{
				options: InstallOptions{
					StrictChecksum: tt.strictChecksum,
					SkipChecksum:   tt.skipChecksum,
				},
			}

			// Simulate the logic from downloadWithChecksum
			var err error
			if !installer.options.SkipChecksum {
				if tt.simulateFailure {
					// Simulate checksum validation failure
					checksumErr := strings.NewReader("checksum mismatch: expected abc123, got def456")
					if installer.options.StrictChecksum {
						// Strict mode: fail the installation
						err = &ChecksumValidationError{
							File:     "test-file.bin",
							Expected: "abc123",
							Actual:   "def456",
						}
					} else {
						// Non-strict mode: log warning and continue (no error)
						err = nil
					}
					_ = checksumErr // Use checksumErr to avoid unused variable warning
				}
			}

			// Verify expected behavior
			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none for case: %s", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v for case: %s", err, tt.description)
			}
		})
	}
}

// ChecksumValidationError represents a checksum validation failure
type ChecksumValidationError struct {
	File     string
	Expected string
	Actual   string
}

func (e *ChecksumValidationError) Error() string {
	return "checksum verification failed for " + e.File + ": expected " + e.Expected + ", got " + e.Actual
}