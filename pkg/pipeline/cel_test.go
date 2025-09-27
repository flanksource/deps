package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flanksource/clicky/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCELPipelineBasicOperations(t *testing.T) {
	// Create temporary directories for testing
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	workDir := filepath.Join(tmpDir, "work")

	// Create work directory
	require.NoError(t, os.MkdirAll(workDir, 0755))

	// Create test files
	testFile1 := filepath.Join(workDir, "test1.txt")
	testFile2 := filepath.Join(workDir, "test2.txt")
	require.NoError(t, os.WriteFile(testFile1, []byte("test content 1"), 0644))
	require.NoError(t, os.WriteFile(testFile2, []byte("test content 2"), 0644))

	// Create task for logging - nil is acceptable for testing
	var task *task.Task = nil

	t.Run("glob function", func(t *testing.T) {
		// Test glob function
		pipeline := NewCELPipeline([]string{`glob("test*.txt")`})
		require.NotNil(t, pipeline)

		// Copy files to a sandbox for testing
		sandboxDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(sandboxDir, "test1.txt"), []byte("test1"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(sandboxDir, "test2.txt"), []byte("test2"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(sandboxDir, "other.txt"), []byte("other"), 0644))

		// Create pipeline context with sandbox
		ctx := NewPipelineContext(task, sandboxDir, binDir, tmpDir, true)

		// Create CEL environment
		celEnv, err := NewCELPipelineEnvironment(ctx)
		require.NoError(t, err)

		// Evaluate the expression
		result, err := celEnv.Evaluate(`glob("test*.txt")`)
		require.NoError(t, err)

		// For CEL expressions returning lists, the result might be wrapped in CEL types
		// Check if it's a CEL list first
		if celList, ok := result.([]interface{}); ok {
			// Convert interface{} slice to string slice
			files := make([]string, len(celList))
			for i, item := range celList {
				if str, ok := item.(string); ok {
					files[i] = str
				}
			}
			require.Len(t, files, 2)
			assert.Contains(t, files, "test1.txt")
			assert.Contains(t, files, "test2.txt")
		} else if files, ok := result.([]string); ok {
			require.Len(t, files, 2)
			assert.Contains(t, files, "test1.txt")
			assert.Contains(t, files, "test2.txt")
		} else {
			t.Fatalf("Expected []string or []interface{} result, got %T: %v", result, result)
		}
	})

	t.Run("log function", func(t *testing.T) {
		// Create pipeline context
		ctx := NewPipelineContext(task, tmpDir, binDir, tmpDir, true)

		// Create CEL environment
		celEnv, err := NewCELPipelineEnvironment(ctx)
		require.NoError(t, err)

		// Test log function
		result, err := celEnv.Evaluate(`log("info", "test message")`)
		require.NoError(t, err)

		// Result should be true
		success, ok := result.(bool)
		require.True(t, ok, "Expected bool result, got %T", result)
		assert.True(t, success)
	})

	t.Run("fail function", func(t *testing.T) {
		// Create pipeline context
		ctx := NewPipelineContext(task, tmpDir, binDir, tmpDir, true)

		// Create CEL environment
		celEnv, err := NewCELPipelineEnvironment(ctx)
		require.NoError(t, err)

		// Test fail function
		result, err := celEnv.Evaluate(`fail("test failure message")`)
		require.NoError(t, err)

		// Result should be false
		success, ok := result.(bool)
		require.True(t, ok, "Expected bool result, got %T", result)
		assert.False(t, success)

		// Pipeline context should be marked as failed
		assert.True(t, ctx.CheckFailed())
		assert.Equal(t, "test failure message", ctx.GetFailureMessage())
	})
}

func TestNewCELPipeline(t *testing.T) {
	t.Run("empty expression list", func(t *testing.T) {
		pipeline := NewCELPipeline([]string{})
		assert.Nil(t, pipeline)
	})

	t.Run("single expression", func(t *testing.T) {
		pipeline := NewCELPipeline([]string{`glob("*.txt")`})
		require.NotNil(t, pipeline)
		assert.Equal(t, `glob("*.txt")`, pipeline.RawExpression)
		require.Len(t, pipeline.Expressions, 1)
		assert.Equal(t, `glob("*.txt")`, pipeline.Expressions[0])
	})

	t.Run("multiple expressions", func(t *testing.T) {
		pipeline := NewCELPipeline([]string{`glob("*.txt")`, `log("info", "found files")`})
		require.NotNil(t, pipeline)
		require.Len(t, pipeline.Expressions, 2)
		assert.Equal(t, `glob("*.txt")`, pipeline.Expressions[0])
		assert.Equal(t, `log("info", "found files")`, pipeline.Expressions[1])
		assert.Equal(t, `glob("*.txt"); log("info", "found files")`, pipeline.RawExpression)
	})

	t.Run("expressions with whitespace", func(t *testing.T) {
		pipeline := NewCELPipeline([]string{`  glob("*.txt")  `, `   log("info", "test")   `})
		require.NotNil(t, pipeline)
		require.Len(t, pipeline.Expressions, 2)
		assert.Equal(t, `glob("*.txt")`, pipeline.Expressions[0])
		assert.Equal(t, `log("info", "test")`, pipeline.Expressions[1])
	})
}

func TestCELPipelineEvaluator(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	workDir := filepath.Join(tmpDir, "work")

	// Create task for logging - nil is acceptable for testing
	var task *task.Task = nil

	// Create evaluator
	evaluator := NewCELPipelineEvaluator(workDir, binDir, tmpDir, task, true)

	t.Run("execute empty pipeline", func(t *testing.T) {
		err := evaluator.Execute(nil)
		require.NoError(t, err)
	})

	t.Run("execute simple pipeline", func(t *testing.T) {
		pipeline := NewCELPipeline([]string{`log("info", "test pipeline")`})
		require.NotNil(t, pipeline)

		err := evaluator.Execute(pipeline)
		require.NoError(t, err)
	})

	t.Run("execute pipeline with failure", func(t *testing.T) {
		pipeline := NewCELPipeline([]string{`fail("intentional failure")`})
		require.NotNil(t, pipeline)

		err := evaluator.Execute(pipeline)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "intentional failure")
	})
}

func TestCalculateDirectoryStats(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		fileCount, totalSize, err := calculateDirectoryStats(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, 0, fileCount)
		assert.Equal(t, int64(0), totalSize)
	})

	t.Run("directory with files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create test files with known content
		file1Content := []byte("Hello World")
		file2Content := []byte("Test content for stats")

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), file1Content, 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), file2Content, 0644))

		// Create a subdirectory with a file
		subDir := filepath.Join(tmpDir, "subdir")
		require.NoError(t, os.MkdirAll(subDir, 0755))
		file3Content := []byte("Nested file")
		require.NoError(t, os.WriteFile(filepath.Join(subDir, "file3.txt"), file3Content, 0644))

		fileCount, totalSize, err := calculateDirectoryStats(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, 3, fileCount) // Should count all files including nested ones
		expectedSize := int64(len(file1Content) + len(file2Content) + len(file3Content))
		assert.Equal(t, expectedSize, totalSize)
	})

	t.Run("directory with subdirectories", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create nested directory structure
		subDir1 := filepath.Join(tmpDir, "sub1")
		subDir2 := filepath.Join(tmpDir, "sub1", "sub2")
		require.NoError(t, os.MkdirAll(subDir2, 0755))

		// Create files at different levels
		fileContent := []byte("test")
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "root.txt"), fileContent, 0644))
		require.NoError(t, os.WriteFile(filepath.Join(subDir1, "sub1.txt"), fileContent, 0644))
		require.NoError(t, os.WriteFile(filepath.Join(subDir2, "sub2.txt"), fileContent, 0644))

		fileCount, totalSize, err := calculateDirectoryStats(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, 3, fileCount)
		assert.Equal(t, int64(len(fileContent)*3), totalSize)
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		fileCount, totalSize, err := calculateDirectoryStats("/nonexistent/path")
		require.Error(t, err)
		assert.Equal(t, 0, fileCount)
		assert.Equal(t, int64(0), totalSize)
	})
}

func TestListDirectoryFiles(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		files := listDirectoryFiles(tmpDir)
		assert.Empty(t, files)
	})

	t.Run("directory with files and subdirectories", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create files and subdirectories
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("nested"), 0644))

		files := listDirectoryFiles(tmpDir)
		// Should only include files, not directories
		assert.Len(t, files, 2)
		assert.Contains(t, files, "file1.txt")
		assert.Contains(t, files, "file2.txt")
		assert.NotContains(t, files, "subdir") // Should not include directories
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		files := listDirectoryFiles("/nonexistent/path")
		assert.Empty(t, files) // Should return empty slice, not error
	})
}

func TestFormatFileList(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		result := formatFileList([]string{})
		assert.Equal(t, "none", result)
	})

	t.Run("single file", func(t *testing.T) {
		result := formatFileList([]string{"file1.txt"})
		assert.Equal(t, "file1.txt", result)
	})

	t.Run("few files", func(t *testing.T) {
		files := []string{"file1.txt", "file2.txt", "file3.txt"}
		result := formatFileList(files)
		assert.Equal(t, "file1.txt, file2.txt, file3.txt", result)
	})

	t.Run("many files - truncated", func(t *testing.T) {
		files := []string{"file1.txt", "file2.txt", "file3.txt", "file4.txt", "file5.txt", "file6.txt", "file7.txt"}
		result := formatFileList(files)
		expected := "file1.txt, file2.txt, file3.txt, file4.txt, file5.txt and 2 more"
		assert.Equal(t, expected, result)
	})

	t.Run("exactly max files", func(t *testing.T) {
		files := []string{"file1.txt", "file2.txt", "file3.txt", "file4.txt", "file5.txt"}
		result := formatFileList(files)
		assert.Equal(t, "file1.txt, file2.txt, file3.txt, file4.txt, file5.txt", result)
	})
}

func TestParseGlobPattern(t *testing.T) {
	t.Run("pattern without type suffix", func(t *testing.T) {
		pattern, typeFilter := parseGlobPattern("*.txt")
		assert.Equal(t, "*.txt", pattern)
		assert.Equal(t, "", typeFilter)
	})

	t.Run("pattern with dir type", func(t *testing.T) {
		pattern, typeFilter := parseGlobPattern("*:dir")
		assert.Equal(t, "*", pattern)
		assert.Equal(t, "dir", typeFilter)
	})

	t.Run("pattern with exec type", func(t *testing.T) {
		pattern, typeFilter := parseGlobPattern("sub*:exec")
		assert.Equal(t, "sub*", pattern)
		assert.Equal(t, "exec", typeFilter)
	})

	t.Run("pattern with archive type", func(t *testing.T) {
		pattern, typeFilter := parseGlobPattern("*.tar.gz:archive")
		assert.Equal(t, "*.tar.gz", pattern)
		assert.Equal(t, "archive", typeFilter)
	})

	t.Run("pattern with multiple colons - last part is type", func(t *testing.T) {
		pattern, typeFilter := parseGlobPattern("file:with:colons:dir")
		assert.Equal(t, "file:with:colons", pattern)
		assert.Equal(t, "dir", typeFilter)
	})

	t.Run("pattern with empty type", func(t *testing.T) {
		pattern, typeFilter := parseGlobPattern("*.txt:")
		assert.Equal(t, "*.txt", pattern)
		assert.Equal(t, "", typeFilter)
	})
}

func TestListDirectoryItems(t *testing.T) {
	t.Run("list files only", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create files and directories
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.py"), []byte("content"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir1"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir2"), 0755))

		items := listDirectoryItems(tmpDir, "files")
		assert.Len(t, items, 2)
		assert.Contains(t, items, "file1.txt")
		assert.Contains(t, items, "file2.py")
		assert.NotContains(t, items, "subdir1")
		assert.NotContains(t, items, "subdir2")
	})

	t.Run("list directories only", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create files and directories
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file2.py"), []byte("content"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir1"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir2"), 0755))

		items := listDirectoryItems(tmpDir, "dirs")
		assert.Len(t, items, 2)
		assert.Contains(t, items, "subdir1")
		assert.Contains(t, items, "subdir2")
		assert.NotContains(t, items, "file1.txt")
		assert.NotContains(t, items, "file2.py")
	})

	t.Run("list all items", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create files and directories
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir1"), 0755))

		items := listDirectoryItems(tmpDir, "all")
		assert.Len(t, items, 2)
		assert.Contains(t, items, "file1.txt")
		assert.Contains(t, items, "subdir1")
	})

	t.Run("default behavior - files only", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create files and directories
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "subdir1"), 0755))

		items := listDirectoryItems(tmpDir, "unknown")
		assert.Len(t, items, 1)
		assert.Contains(t, items, "file1.txt")
		assert.NotContains(t, items, "subdir1")
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		items := listDirectoryItems("/nonexistent/path", "files")
		assert.Empty(t, items)
	})
}
