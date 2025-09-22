package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePipeline(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected []Operation
		wantErr  bool
	}{
		{
			name: "simple operation",
			expr: "cleanup()",
			expected: []Operation{
				{Name: "cleanup", Args: []string{}},
			},
		},
		{
			name: "single argument",
			expr: "unarchive(\"*.tar.gz\")",
			expected: []Operation{
				{Name: "unarchive", Args: []string{"*.tar.gz"}},
			},
		},
		{
			name: "nested function call",
			expr: "unarchive(glob(\"*.txz\"))",
			expected: []Operation{
				{Name: "unarchive", Args: []string{"glob(\"*.txz\")"}},
			},
		},
		{
			name: "multiple operations",
			expr: "unarchive(glob(\"*.txz\")) && chdir(glob(\"*:dir\"))",
			expected: []Operation{
				{Name: "unarchive", Args: []string{"glob(\"*.txz\")"}},
				{Name: "chdir", Args: []string{"glob(\"*:dir\")"}},
			},
		},
		{
			name: "complex pipeline",
			expr: "unarchive(glob(\"*.txz\")) && chdir(glob(\"*:dir\")) && cleanup()",
			expected: []Operation{
				{Name: "unarchive", Args: []string{"glob(\"*.txz\")"}},
				{Name: "chdir", Args: []string{"glob(\"*:dir\")"}},
				{Name: "cleanup", Args: []string{}},
			},
		},
		{
			name: "multiline pipeline",
			expr: `
				unarchive(glob("*.txz")) &&
				chdir(glob("*:dir")) &&
				cleanup()
			`,
			expected: []Operation{
				{Name: "unarchive", Args: []string{"glob(\"*.txz\")"}},
				{Name: "chdir", Args: []string{"glob(\"*:dir\")"}},
				{Name: "cleanup", Args: []string{}},
			},
		},
		{
			name: "move operation with two args",
			expr: "move(\"src/file.txt\", \"dest/file.txt\")",
			expected: []Operation{
				{Name: "move", Args: []string{"src/file.txt", "dest/file.txt"}},
			},
		},
		{
			name: "chmod with mode",
			expr: "chmod(glob(\"*\"), \"0755\")",
			expected: []Operation{
				{Name: "chmod", Args: []string{"glob(\"*\")", "0755"}},
			},
		},
		{
			name:    "empty expression",
			expr:    "",
			wantErr: false,
		},
		{
			name:    "unmatched parenthesis",
			expr:    "unarchive(glob(\"*.txz\"",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline, err := ParsePipeline(tt.expr)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.expr == "" {
				assert.Nil(t, pipeline)
				return
			}

			require.NotNil(t, pipeline)
			assert.Equal(t, len(tt.expected), len(pipeline.Operations))

			for i, expected := range tt.expected {
				assert.Equal(t, expected.Name, pipeline.Operations[i].Name)
				assert.Equal(t, expected.Args, pipeline.Operations[i].Args)
			}
		})
	}
}

func TestParseArguments(t *testing.T) {
	tests := []struct {
		name     string
		argsStr  string
		expected []string
	}{
		{
			name:     "single argument",
			argsStr:  "\"*.tar.gz\"",
			expected: []string{"*.tar.gz"},
		},
		{
			name:     "multiple arguments",
			argsStr:  "\"src\", \"dest\"",
			expected: []string{"src", "dest"},
		},
		{
			name:     "nested function",
			argsStr:  "glob(\"*.txz\")",
			expected: []string{"glob(\"*.txz\")"},
		},
		{
			name:     "complex nested",
			argsStr:  "glob(\"*.txz\"), \"0755\"",
			expected: []string{"glob(\"*.txz\")", "0755"},
		},
		{
			name:     "single quotes",
			argsStr:  "'file.txt'",
			expected: []string{"file.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseArguments(tt.argsStr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOperationType_IsValid(t *testing.T) {
	tests := []struct {
		op    OperationType
		valid bool
	}{
		{OpUnarchive, true},
		{OpChdir, true},
		{OpGlob, true},
		{OpCleanup, true},
		{OpMove, true},
		{OpDelete, true},
		{OpChmod, true},
		{OperationType("invalid"), false},
		{OperationType("exec"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.op), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.op.IsValid())
		})
	}
}

func TestSecurityValidation(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		sandbox   string
		wantErr   bool
	}{
		{
			name:    "valid path within sandbox",
			path:    "/tmp/sandbox/file.txt",
			sandbox: "/tmp/sandbox",
			wantErr: false,
		},
		{
			name:    "path traversal attempt",
			path:    "/tmp/sandbox/../../../etc/passwd",
			sandbox: "/tmp/sandbox",
			wantErr: true,
		},
		{
			name:    "outside sandbox",
			path:    "/etc/passwd",
			sandbox: "/tmp/sandbox",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSandboxPath(tt.path, tt.sandbox)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsArchive(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"file.tar.gz", true},
		{"file.tgz", true},
		{"file.tar.xz", true},
		{"file.txz", true},
		{"file.zip", true},
		{"file.jar", true},
		{"file.tar", true},
		{"file.txt", false},
		{"binary", false},
		{"FILE.TAR.GZ", true}, // Case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, isArchive(tt.path))
		})
	}
}

func TestStripTypeSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"*:dir", "*"},
		{"postgres*:dir", "postgres*"},
		{"*:executable", "*"},
		{"file.txt", "file.txt"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripTypeSuffix(tt.input))
		})
	}
}

// Integration test for pipeline execution
func TestPipelineExecution(t *testing.T) {
	// This test requires creating actual files and directories
	t.Run("simple cleanup pipeline", func(t *testing.T) {
		// Create temp directory
		tempDir, err := os.MkdirTemp("", "pipeline-test-")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Create some test files
		testFile := filepath.Join(tempDir, "test.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

		tempFile := filepath.Join(tempDir, "temp.tmp")
		require.NoError(t, os.WriteFile(tempFile, []byte("temp"), 0644))

		// Parse pipeline to validate syntax
		_, err = ParsePipeline("cleanup()")
		require.NoError(t, err)

		// Execute
		processor := NewProcessor(tempDir, tempDir, nil, false)
		processor.sandboxDir = tempDir // Override for test

		op := Operation{Name: "cleanup", Args: []string{}}
		_, err = processor.executeOperation(op)
		require.NoError(t, err)

		// Check that temp file was removed
		_, err = os.Stat(tempFile)
		assert.True(t, os.IsNotExist(err))

		// Regular file should still exist
		_, err = os.Stat(testFile)
		assert.NoError(t, err)
	})
}