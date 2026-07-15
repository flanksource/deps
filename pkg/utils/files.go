package utils

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// renameFile is a seam over os.Rename so the cross-device fallback in Move can
// be exercised in tests without requiring two real filesystems.
var renameFile = os.Rename

// CopyFile copies a file from src to dst
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// Move moves src to dst. It attempts an atomic os.Rename first and falls back to
// a recursive copy + remove when src and dst are on different filesystems
// (rename returns EXDEV, "invalid cross-device link"). Any other rename error is
// returned unchanged.
func Move(src, dst string) error {
	err := renameFile(src, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := CopyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// CopyDir recursively copies the tree rooted at src to dst, preserving file
// permission bits and symlinks (symlinks are recreated, never dereferenced).
func CopyDir(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	return copyPath(src, dst, info)
}

func copyPath(src, dst string, info os.FileInfo) error {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.IsDir():
		return copyDirContents(src, dst, info.Mode().Perm())
	default:
		return copyRegularFile(src, dst, info.Mode().Perm())
	}
}

func copyDirContents(src, dst string, perm os.FileMode) error {
	if err := os.MkdirAll(dst, perm); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), info); err != nil {
			return err
		}
	}
	return nil
}

func copyRegularFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
