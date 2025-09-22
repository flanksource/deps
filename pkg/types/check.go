package types

// CheckStatus represents the status of a version check
type CheckStatus string

const (
	CheckStatusOK       CheckStatus = "OK"
	CheckStatusOutdated CheckStatus = "OUTDATED"
	CheckStatusNewer    CheckStatus = "NEWER"
	CheckStatusMissing  CheckStatus = "MISSING"
	CheckStatusError    CheckStatus = "ERROR"
	CheckStatusUnknown  CheckStatus = "UNKNOWN"
)

// ChecksumStatus represents the status of a checksum verification
type ChecksumStatus string

const (
	ChecksumStatusOK       ChecksumStatus = "OK"
	ChecksumStatusMismatch ChecksumStatus = "MISMATCH"
	ChecksumStatusUnknown  ChecksumStatus = "UNKNOWN"
	ChecksumStatusSkipped  ChecksumStatus = "SKIPPED"
	ChecksumStatusError    ChecksumStatus = "ERROR"
)

// CheckResult represents the result of checking a tool's version
type CheckResult struct {
	Tool             string         `json:"tool"`
	InstalledVersion string         `json:"installed_version,omitempty"`
	ExpectedVersion  string         `json:"expected_version,omitempty"`
	RequestedVersion string         `json:"requested_version,omitempty"`
	Status           CheckStatus    `json:"status"`
	Error            string         `json:"error,omitempty"`
	BinaryPath       string         `json:"binary_path,omitempty"`

	// Checksum verification fields
	ChecksumStatus     ChecksumStatus `json:"checksum_status,omitempty"`
	ExpectedChecksum   string         `json:"expected_checksum,omitempty"`
	ActualChecksum     string         `json:"actual_checksum,omitempty"`
	ChecksumType       string         `json:"checksum_type,omitempty"`
	ChecksumError      string         `json:"checksum_error,omitempty"`
	ChecksumSource     string         `json:"checksum_source,omitempty"`
}

// CheckSummary represents a summary of all check results
type CheckSummary struct {
	Total    int `json:"total"`
	OK       int `json:"ok"`
	Outdated int `json:"outdated"`
	Newer    int `json:"newer"`
	Missing  int `json:"missing"`
	Errors   int `json:"errors"`
	Unknown  int `json:"unknown"`

	// Checksum verification summary
	ChecksumVerified int `json:"checksum_verified,omitempty"`
	ChecksumMismatch int `json:"checksum_mismatch,omitempty"`
	ChecksumError    int `json:"checksum_error,omitempty"`
	ChecksumSkipped  int `json:"checksum_skipped,omitempty"`
}

// AddResult adds a check result to the summary
func (s *CheckSummary) AddResult(result CheckResult) {
	s.Total++
	switch result.Status {
	case CheckStatusOK:
		s.OK++
	case CheckStatusOutdated:
		s.Outdated++
	case CheckStatusNewer:
		s.Newer++
	case CheckStatusMissing:
		s.Missing++
	case CheckStatusError:
		s.Errors++
	case CheckStatusUnknown:
		s.Unknown++
	}

	// Add checksum verification summary
	switch result.ChecksumStatus {
	case ChecksumStatusOK:
		s.ChecksumVerified++
	case ChecksumStatusMismatch:
		s.ChecksumMismatch++
	case ChecksumStatusError:
		s.ChecksumError++
	case ChecksumStatusSkipped:
		s.ChecksumSkipped++
	}
}