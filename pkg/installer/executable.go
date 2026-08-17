package installer

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/flanksource/deps/pkg/platform"
)

// executableFormat is the object file format a platform can execute.
type executableFormat string

const (
	formatELF    executableFormat = "ELF"
	formatMachO  executableFormat = "Mach-O"
	formatPE     executableFormat = "PE"
	formatScript executableFormat = "script"
	formatDylib  executableFormat = "Mach-O universal binary"
)

// executableMagics maps leading bytes to the format they identify.
var executableMagics = []struct {
	magic  []byte
	format executableFormat
}{
	{[]byte{0x7f, 'E', 'L', 'F'}, formatELF},
	{[]byte{0xfe, 0xed, 0xfa, 0xce}, formatMachO},
	{[]byte{0xfe, 0xed, 0xfa, 0xcf}, formatMachO},
	{[]byte{0xce, 0xfa, 0xed, 0xfe}, formatMachO},
	{[]byte{0xcf, 0xfa, 0xed, 0xfe}, formatMachO},
	{[]byte{0xca, 0xfe, 0xba, 0xbe}, formatDylib},
	{[]byte{0xbe, 0xba, 0xfe, 0xca}, formatDylib},
	{[]byte{'M', 'Z'}, formatPE},
	{[]byte{'#', '!'}, formatScript},
}

// VerifyExecutable fails when the installed file is not something the target platform
// can run. It inspects the file header rather than executing it, so it stays valid for
// cross-platform installs. This is the guard against installing a checksum or error
// page that was mistaken for a release binary.
func VerifyExecutable(path, assetName string, plat platform.Platform) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("installed file %s is unreadable: %w", path, err)
	}
	if info.IsDir() {
		return nil
	}

	subject := assetName
	if subject == "" {
		subject = info.Name()
	}

	if info.Size() == 0 {
		return fmt.Errorf("installed asset %s is empty", subject)
	}

	header := make([]byte, 256)
	n, err := readHeader(path, header)
	if err != nil {
		return fmt.Errorf("installed file %s is unreadable: %w", path, err)
	}
	header = header[:n]

	expected := expectedExecutableFormat(plat.OS)
	switch detected := detectExecutableFormat(header); {
	case detected == formatScript, detected == expected:
		return nil
	case detected == formatDylib && expected == formatMachO:
		return nil
	case detected != "":
		return fmt.Errorf("installed asset %s is a %s binary, but %s needs %s",
			subject, detected, plat.String(), expected)
	default:
		return fmt.Errorf("installed asset %s is not an executable (%s expects %s); content starts %q",
			subject, plat.String(), expected, contentPreview(header))
	}
}

func readHeader(path string, buf []byte) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()

	n, err := file.Read(buf)
	if err != nil && n == 0 {
		return 0, err
	}
	return n, nil
}

func detectExecutableFormat(header []byte) executableFormat {
	for _, candidate := range executableMagics {
		if bytes.HasPrefix(header, candidate.magic) {
			return candidate.format
		}
	}
	return ""
}

func expectedExecutableFormat(os string) executableFormat {
	switch os {
	case "darwin":
		return formatMachO
	case "windows":
		return formatPE
	default:
		return formatELF
	}
}

// contentPreview renders the start of a file for an error message, so the user can see
// what was downloaded instead of a binary.
func contentPreview(header []byte) string {
	preview := string(header)
	if line, _, found := strings.Cut(preview, "\n"); found {
		preview = line
	}
	preview = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return '.'
	}, strings.TrimSpace(preview))

	const maxPreview = 60
	if len(preview) > maxPreview {
		preview = preview[:maxPreview] + "…"
	}
	return preview
}
