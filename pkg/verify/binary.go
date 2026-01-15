package verify

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"os"
)

// BinaryInfo contains detected OS and architecture of a binary
type BinaryInfo struct {
	OS   string
	Arch string
	Type string // "elf", "macho", "pe", "dotnet", "unknown"
}

// DetectBinaryPlatform detects the OS and architecture of a binary file
func DetectBinaryPlatform(path string) (*BinaryInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Read magic bytes
	magic := make([]byte, 4)
	if _, err := f.Read(magic); err != nil {
		return nil, fmt.Errorf("failed to read magic bytes: %w", err)
	}

	// Reset file position
	if _, err := f.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to seek: %w", err)
	}

	// Try ELF (Linux)
	if magic[0] == 0x7f && magic[1] == 'E' && magic[2] == 'L' && magic[3] == 'F' {
		return detectELF(path)
	}

	// Try Mach-O (macOS) - various magic numbers
	if (magic[0] == 0xfe && magic[1] == 0xed && magic[2] == 0xfa && magic[3] == 0xce) || // 32-bit
		(magic[0] == 0xfe && magic[1] == 0xed && magic[2] == 0xfa && magic[3] == 0xcf) || // 64-bit
		(magic[0] == 0xce && magic[1] == 0xfa && magic[2] == 0xed && magic[3] == 0xfe) || // 32-bit swapped
		(magic[0] == 0xcf && magic[1] == 0xfa && magic[2] == 0xed && magic[3] == 0xfe) || // 64-bit swapped
		(magic[0] == 0xca && magic[1] == 0xfe && magic[2] == 0xba && magic[3] == 0xbe) { // Universal
		return detectMachO(path)
	}

	// Try PE (Windows)
	if magic[0] == 'M' && magic[1] == 'Z' {
		return detectPE(path)
	}

	return &BinaryInfo{Type: "unknown"}, nil
}

func detectELF(path string) (*BinaryInfo, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ELF: %w", err)
	}
	defer func() { _ = f.Close() }()

	info := &BinaryInfo{
		OS:   "linux",
		Type: "elf",
	}

	switch f.Machine {
	case elf.EM_X86_64:
		info.Arch = "amd64"
	case elf.EM_AARCH64:
		info.Arch = "arm64"
	case elf.EM_386:
		info.Arch = "386"
	case elf.EM_ARM:
		info.Arch = "arm"
	case elf.EM_PPC64:
		info.Arch = "ppc64"
	case elf.EM_S390:
		info.Arch = "s390x"
	case elf.EM_RISCV:
		info.Arch = "riscv64"
	default:
		info.Arch = fmt.Sprintf("unknown(%d)", f.Machine)
	}

	return info, nil
}

func detectMachO(path string) (*BinaryInfo, error) {
	f, err := macho.Open(path)
	if err != nil {
		// Try as fat/universal binary
		fatFile, fatErr := macho.OpenFat(path)
		if fatErr != nil {
			return nil, fmt.Errorf("failed to parse Mach-O: %w", err)
		}
		defer func() { _ = fatFile.Close() }()

		// Return info for first arch in universal binary
		if len(fatFile.Arches) > 0 {
			return machoCpuToInfo(fatFile.Arches[0].Cpu), nil
		}
		return &BinaryInfo{OS: "darwin", Type: "macho", Arch: "universal"}, nil
	}
	defer func() { _ = f.Close() }()

	return machoCpuToInfo(f.Cpu), nil
}

func machoCpuToInfo(cpu macho.Cpu) *BinaryInfo {
	info := &BinaryInfo{
		OS:   "darwin",
		Type: "macho",
	}

	switch cpu {
	case macho.CpuAmd64:
		info.Arch = "amd64"
	case macho.CpuArm64:
		info.Arch = "arm64"
	case macho.Cpu386:
		info.Arch = "386"
	case macho.CpuArm:
		info.Arch = "arm"
	case macho.CpuPpc64:
		info.Arch = "ppc64"
	default:
		info.Arch = fmt.Sprintf("unknown(%d)", cpu)
	}

	return info
}

func detectPE(path string) (*BinaryInfo, error) {
	f, err := pe.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PE: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Check for .NET assembly by looking for CLR runtime header
	isDotNet := false
	if optHdr, ok := f.OptionalHeader.(*pe.OptionalHeader32); ok {
		if len(optHdr.DataDirectory) > 14 && optHdr.DataDirectory[14].Size > 0 {
			isDotNet = true
		}
	} else if optHdr, ok := f.OptionalHeader.(*pe.OptionalHeader64); ok {
		if len(optHdr.DataDirectory) > 14 && optHdr.DataDirectory[14].Size > 0 {
			isDotNet = true
		}
	}

	info := &BinaryInfo{
		OS:   "windows",
		Type: "pe",
	}

	// .NET assemblies are cross-platform via CLR runtime
	if isDotNet {
		info.Type = "dotnet"
	}

	switch f.Machine {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		info.Arch = "amd64"
	case pe.IMAGE_FILE_MACHINE_ARM64:
		info.Arch = "arm64"
	case pe.IMAGE_FILE_MACHINE_I386:
		info.Arch = "386"
	case pe.IMAGE_FILE_MACHINE_ARMNT:
		info.Arch = "arm"
	default:
		info.Arch = fmt.Sprintf("unknown(%d)", f.Machine)
	}

	return info, nil
}

// VerifyBinaryPlatform checks if a binary matches the expected OS and architecture
func VerifyBinaryPlatform(path, expectedOS, expectedArch string) error {
	info, err := DetectBinaryPlatform(path)
	if err != nil {
		return fmt.Errorf("failed to detect binary platform: %w", err)
	}

	if info.Type == "unknown" {
		return fmt.Errorf("unknown binary format")
	}

	if info.OS != expectedOS {
		return fmt.Errorf("binary OS mismatch: expected %s, got %s", expectedOS, info.OS)
	}

	if info.Arch != expectedArch {
		return fmt.Errorf("binary arch mismatch: expected %s, got %s", expectedArch, info.Arch)
	}

	return nil
}
