package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// InstallDmg installs the package inside a macOS disk image.
//
// A .dmg is a container, not an installer: it is mounted, the .pkg inside it is
// installed, and it is unmounted again. Chef and CINC ship their macOS omnibus
// builds this way, so unwrapping here is what lets them reach the same
// system-installer path as a bare .pkg.
func InstallDmg(dmgPath, destDir string, opts *SystemInstallOptions) (*SystemInstallResult, error) {
	result := &SystemInstallResult{
		RequiredSudo: true,
		SystemWide:   true,
		ToolName:     opts.ToolName,
	}

	if runtime.GOOS != "darwin" {
		return result, fmt.Errorf(".dmg files can only be mounted on macOS")
	}

	// Confirm before mounting, and name the file the user actually downloaded
	// rather than the .pkg they never asked for. The inner install then runs
	// silently so this is the only prompt.
	if !opts.Silent {
		displaySystemWideWarning(opts.ToolName, dmgPath)
		if !promptForConfirmation("Continue? [y/N]: ") {
			return result, fmt.Errorf("installation cancelled by user")
		}
	}

	mountPoint, unmount, err := mountImage(dmgPath)
	if err != nil {
		return result, err
	}
	defer func() {
		if err := unmount(); err != nil && opts.Task != nil {
			// A failed unmount leaves a volume attached but does not undo a
			// successful install, so it is reported rather than returned.
			opts.Task.Warnf("%v", err)
		}
	}()

	if opts.Task != nil {
		opts.Task.V(3).Infof("Mounted %s at %s", filepath.Base(dmgPath), mountPoint)
	}

	pkgPath, err := findPackageInImage(mountPoint)
	if err != nil {
		return result, err
	}

	// Silent: the confirmation above already covered this install.
	inner := *opts
	inner.Silent = true
	return InstallPkg(pkgPath, destDir, &inner)
}

// mountImage attaches a disk image read-only and returns its mount point along
// with the function that releases it. The caller must always call unmount: an
// image left attached holds the file open and shows up as a mounted volume.
func mountImage(dmgPath string) (mountPoint string, unmount func() error, err error) {
	mountPoint, err = os.MkdirTemp("", "deps-dmg-")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create mount point: %w", err)
	}

	// -nobrowse keeps the volume out of Finder, -readonly means nothing done
	// here can modify the downloaded image.
	attach := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", mountPoint, dmgPath)
	if output, err := attach.CombinedOutput(); err != nil {
		_ = os.RemoveAll(mountPoint)
		return "", nil, fmt.Errorf("failed to mount %s: %w: %s", filepath.Base(dmgPath), err, output)
	}

	return mountPoint, func() error {
		detach := exec.Command("hdiutil", "detach", mountPoint)
		output, err := detach.CombinedOutput()
		// The directory is only removed once the volume is detached; removing it
		// while still attached would strand the mount.
		if err != nil {
			return fmt.Errorf("failed to unmount %s: %w: %s", mountPoint, err, output)
		}
		return os.RemoveAll(mountPoint)
	}, nil
}

// findPackageInImage locates the single .pkg in a mounted disk image.
//
// More than one is ambiguous rather than a matter of picking the first: which
// package a multi-package image expects you to install is not something the
// filenames say, and guessing would install the wrong software.
func findPackageInImage(mountPoint string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(mountPoint, "*.pkg"))
	if err != nil {
		return "", fmt.Errorf("failed to search disk image: %w", err)
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no .pkg found in disk image at %s", mountPoint)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, match := range matches {
			names = append(names, filepath.Base(match))
		}
		return "", fmt.Errorf(
			"disk image contains %d packages (%v): deps cannot tell which one to install",
			len(matches), names)
	}
}
