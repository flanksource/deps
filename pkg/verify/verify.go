package verify

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flanksource/deps/pkg/checksum"
	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
)

// ChecksumResult represents the result of checksum verification
type ChecksumResult struct {
	ChecksumStatus   types.ChecksumStatus
	ExpectedChecksum string
	ActualChecksum   string
	ChecksumType     string
	ChecksumError    string
	ChecksumSource   string
}

// VerifyBinaryChecksum verifies the checksum of an installed binary
func VerifyBinaryChecksum(tool string, pkg types.Package, binDir string, lockFile *types.LockFile, plat platform.Platform) ChecksumResult {
	result := ChecksumResult{
		ChecksumStatus: types.ChecksumStatusUnknown,
	}

	// Build binary path
	binaryName := tool
	if pkg.BinaryName != "" {
		binaryName = pkg.BinaryName
	}
	if plat.IsWindows() && filepath.Ext(binaryName) != ".exe" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(binDir, binaryName)

	// Check if binary exists
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		result.ChecksumStatus = types.ChecksumStatusError
		result.ChecksumError = "binary not found"
		return result
	}

	// Try to get expected checksum from lock file first
	expectedChecksum := ""
	checksumSource := ""

	if lockFile != nil {
		if lockEntry, exists := lockFile.Dependencies[tool]; exists {
			platformKey := fmt.Sprintf("%s-%s", plat.OS, plat.Arch)
			if platformEntry, exists := lockEntry.Platforms[platformKey]; exists && platformEntry.Checksum != "" {
				expectedChecksum = platformEntry.Checksum
				checksumSource = "deps-lock.yaml"
			}
		}
	}

	// If no checksum in lock file, try to discover from source
	if expectedChecksum == "" {
		// TODO: Implement checksum discovery from source
		// This would use the existing Discovery strategies but needs resolution context
		result.ChecksumStatus = types.ChecksumStatusUnknown
		result.ChecksumError = "no expected checksum available"
		return result
	}

	result.ExpectedChecksum = expectedChecksum
	result.ChecksumSource = checksumSource

	// Parse expected checksum to get type
	expectedValue, hashType := checksum.ParseChecksum(expectedChecksum)
	result.ChecksumType = string(hashType)

	// Calculate actual checksum
	actualValue, err := checksum.CalculateBinaryChecksum(binaryPath, hashType)
	if err != nil {
		result.ChecksumStatus = types.ChecksumStatusError
		result.ChecksumError = fmt.Sprintf("failed to calculate checksum: %v", err)
		return result
	}

	result.ActualChecksum = actualValue

	// Compare checksums
	if actualValue == expectedValue {
		result.ChecksumStatus = types.ChecksumStatusOK
	} else {
		result.ChecksumStatus = types.ChecksumStatusMismatch
		result.ChecksumError = fmt.Sprintf("expected %s, got %s", expectedValue, actualValue)
	}

	return result
}

// FormatChecksumStatus formats a checksum status for display
func FormatChecksumStatus(status types.ChecksumStatus) string {
	switch status {
	case types.ChecksumStatusOK:
		return "✅ VERIFIED"
	case types.ChecksumStatusMismatch:
		return "❌ MISMATCH"
	case types.ChecksumStatusError:
		return "🚫 ERROR"
	case types.ChecksumStatusUnknown:
		return "❓ UNKNOWN"
	case types.ChecksumStatusSkipped:
		return "-"
	default:
		return string(status)
	}
}