package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flanksource/clicky/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectoryMovingOptimization(t *testing.T) {
	t.Run("moveAllContents should move entire directory as single unit", func(t *testing.T) {
		// Setup
		tmpDir := t.TempDir()
		workDir := filepath.Join(tmpDir, "workdir")
		targetDir := filepath.Join(tmpDir, "target")

		// Create workdir with multiple items
		require.NoError(t, os.MkdirAll(workDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "file1.txt"), []byte("content1"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "file2.txt"), []byte("content2"), 0644))

		subDir := filepath.Join(workDir, "subdir")
		require.NoError(t, os.MkdirAll(subDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested"), 0644))

		// Read entries to pass to function (mimicking installer behavior)
		entries, err := os.ReadDir(workDir)
		require.NoError(t, err)
		require.Len(t, entries, 3) // file1.txt, file2.txt, subdir

		// Create installer and task
		installer := New()
		task := &task.Task{}

		// Execute moveAllContents
		err = installer.moveAllContents(workDir, targetDir, entries, task)
		require.NoError(t, err)

		// Verify results
		// 1. Target directory should exist and contain all files
		assert.DirExists(t, targetDir)
		assert.FileExists(t, filepath.Join(targetDir, "file1.txt"))
		assert.FileExists(t, filepath.Join(targetDir, "file2.txt"))
		assert.DirExists(t, filepath.Join(targetDir, "subdir"))
		assert.FileExists(t, filepath.Join(targetDir, "subdir", "nested.txt"))

		// 2. Content should be preserved
		content1, err := os.ReadFile(filepath.Join(targetDir, "file1.txt"))
		require.NoError(t, err)
		assert.Equal(t, "content1", string(content1))

		content2, err := os.ReadFile(filepath.Join(targetDir, "subdir", "nested.txt"))
		require.NoError(t, err)
		assert.Equal(t, "nested", string(content2))

		// 3. Original workDir should no longer exist (moved, not copied)
		_, err = os.Stat(workDir)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("moveAllContents should replace existing target directory", func(t *testing.T) {
		// Setup
		tmpDir := t.TempDir()
		workDir := filepath.Join(tmpDir, "workdir")
		targetDir := filepath.Join(tmpDir, "target")

		// Create workdir with new content
		require.NoError(t, os.MkdirAll(workDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "new-file.txt"), []byte("new content"), 0644))

		// Create existing target directory with old content
		require.NoError(t, os.MkdirAll(targetDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "old-file.txt"), []byte("old content"), 0644))

		// Read entries
		entries, err := os.ReadDir(workDir)
		require.NoError(t, err)

		// Create installer and task
		installer := New()
		task := &task.Task{}

		// Execute moveAllContents
		err = installer.moveAllContents(workDir, targetDir, entries, task)
		require.NoError(t, err)

		// Verify results
		// 1. New content should be present
		assert.FileExists(t, filepath.Join(targetDir, "new-file.txt"))
		content, err := os.ReadFile(filepath.Join(targetDir, "new-file.txt"))
		require.NoError(t, err)
		assert.Equal(t, "new content", string(content))

		// 2. Old content should be gone
		assert.NoFileExists(t, filepath.Join(targetDir, "old-file.txt"))

		// 3. Original workDir should no longer exist
		_, err = os.Stat(workDir)
		assert.True(t, os.IsNotExist(err))
	})
}
