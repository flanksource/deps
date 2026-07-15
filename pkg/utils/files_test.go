package utils

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTree creates a representative extracted-archive layout under root:
// a nested directory, a regular file, an executable, and a relative symlink.
func buildTree(t *testing.T, root string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "bin"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "data.txt"), []byte("payload"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "bin", "node"), []byte("#!/bin/sh\n"), 0755))
	require.NoError(t, os.Symlink("../lib/data.txt", filepath.Join(root, "bin", "npx")))
}

// assertTree verifies buildTree's layout was reproduced at root with modes and
// symlinks intact.
func assertTree(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "lib", "data.txt"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(data))

	dataInfo, err := os.Stat(filepath.Join(root, "lib", "data.txt"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0644), dataInfo.Mode().Perm())

	execInfo, err := os.Stat(filepath.Join(root, "bin", "node"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), execInfo.Mode().Perm())

	linkInfo, err := os.Lstat(filepath.Join(root, "bin", "npx"))
	require.NoError(t, err)
	assert.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "npx should remain a symlink, not be dereferenced")

	target, err := os.Readlink(filepath.Join(root, "bin", "npx"))
	require.NoError(t, err)
	assert.Equal(t, "../lib/data.txt", target)
}

func TestCopyDir_PreservesTreeModesAndSymlinks(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	buildTree(t, src)

	require.NoError(t, CopyDir(src, dst))

	assertTree(t, dst)
	// Source must remain untouched by a copy.
	assert.DirExists(t, src)
}

func TestMove_SameFilesystem(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	buildTree(t, src)

	require.NoError(t, Move(src, dst))

	assertTree(t, dst)
	assert.NoDirExists(t, src, "source should be removed after a move")
}

func TestMove_CrossDeviceFallback(t *testing.T) {
	original := renameFile
	t.Cleanup(func() { renameFile = original })
	// Force the cross-device path: rename fails as if src and dst were on
	// different filesystems, exercising the copy + remove fallback.
	renameFile = func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
	}

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dst := filepath.Join(tmp, "dst")
	buildTree(t, src)

	require.NoError(t, Move(src, dst))

	assertTree(t, dst)
	assert.NoDirExists(t, src, "source should be removed after cross-device move")
}
