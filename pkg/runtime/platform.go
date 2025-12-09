package runtime

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// platformInfo provides platform-specific runtime information
type platformInfo struct {
	// isWindows indicates if the platform is Windows
	isWindows bool

	// pathSeparator is the PATH environment variable separator
	pathSeparator string

	// exeExtension is the executable extension (.exe on Windows, empty on Unix)
	exeExtension string
}

var platform = initPlatform()

func initPlatform() platformInfo {
	return platformInfo{
		isWindows:     runtime.GOOS == "windows",
		pathSeparator: string(os.PathListSeparator),
		exeExtension:  getExeExtension(),
	}
}

func getExeExtension() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// getBinaryName returns the platform-specific binary name
// On Windows, appends .exe; on Unix, returns as-is
func getBinaryName(name string) string {
	if platform.isWindows && !strings.HasSuffix(name, platform.exeExtension) {
		return name + platform.exeExtension
	}
	return name
}

// searchPath searches the system PATH for a binary
// Returns the full path to the binary if found
func searchPath(binaryName string) (string, error) {
	// Try with platform-specific extension
	path, err := exec.LookPath(getBinaryName(binaryName))
	if err == nil {
		return path, nil
	}

	// On Unix, also try without extension (for python3, node, etc.)
	if !platform.isWindows {
		path, err = exec.LookPath(binaryName)
		if err == nil {
			return path, nil
		}
	}

	return "", err
}

// findBinaryInPath searches for a binary name in PATH, trying multiple variants
// For example, for Python: python3, python, python.exe
func findBinaryInPath(variants ...string) (string, error) {
	for _, variant := range variants {
		if path, err := searchPath(variant); err == nil {
			return path, nil
		}
	}

	return "", exec.ErrNotFound
}
