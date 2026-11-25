package ghrelease

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/ulikunitz/xz"
)

// archiveExt returns the compound archive extension of an asset name.
func archiveExt(asset string) string {
	for _, ext := range []string{".tar.gz", ".tgz", ".tar.xz", ".zip"} {
		if strings.HasSuffix(asset, ext) {
			return ext
		}
	}
	return ""
}

// extractBinary writes the first archive member whose base name is binary to
// target, executable.
func extractBinary(archive, asset, binary, target string) error {
	var err error
	switch archiveExt(asset) {
	case ".zip":
		err = extractZip(archive, binary, target)
	case ".tar.gz", ".tgz", ".tar.xz":
		err = extractTar(archive, archiveExt(asset) == ".tar.xz", binary, target)
	default:
		return fmt.Errorf("ghrelease: unsupported archive type for asset %s", asset)
	}
	if err == errNotFound {
		return fmt.Errorf("ghrelease: %q not found in %s", binary, asset)
	}
	return err
}

var errNotFound = fmt.Errorf("binary not found in archive")

func extractTar(archive string, isXZ bool, binary, target string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var r io.Reader
	if isXZ {
		r, err = xz.NewReader(f)
	} else {
		r, err = gzip.NewReader(f)
	}
	if err != nil {
		return fmt.Errorf("ghrelease: open archive: %w", err)
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return errNotFound
		}
		if err != nil {
			return fmt.Errorf("ghrelease: read archive: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && path.Base(hdr.Name) == binary {
			return writeExecutable(tr, target)
		}
	}
}

func extractZip(archive, binary, target string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("ghrelease: open archive: %w", err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || path.Base(f.Name) != binary {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		return writeExecutable(rc, target)
	}
	return errNotFound
}

// writeExecutable writes via a sibling temp file and rename, so a concurrent
// reader never sees a partial binary.
func writeExecutable(src io.Reader, target string) error {
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("ghrelease: write %s: %w", target, err)
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}
