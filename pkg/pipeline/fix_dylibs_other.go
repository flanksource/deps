//go:build !darwin && !linux

package pipeline

func detectBrokenDylibs(binaryPath, _ string) ([]DylibRef, error) {
	return nil, nil
}

func machoArchToGoArch(arch string) string {
	return arch
}

func detectArch(path string) string {
	return ""
}
