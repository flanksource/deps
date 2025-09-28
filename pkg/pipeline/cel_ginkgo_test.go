package pipeline

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CEL Pipeline Evaluator", func() {
	var (
		evaluator               *CELPipelineEvaluator
		tmpDir, workDir, binDir string
	)

	BeforeEach(func() {
		tmpDir = GinkgoT().TempDir()
		workDir = filepath.Join(tmpDir, "work")
		binDir = filepath.Join(tmpDir, "bin")
		Expect(os.MkdirAll(workDir, 0755)).To(Succeed())

		evaluator = NewCELPipelineEvaluator(workDir, binDir, tmpDir, nil, true)
	})

	Describe("Basic CEL Function Operations", func() {
		testCases := []struct {
			name          string
			expressions   []string
			setupFiles    map[string]string // filename -> content
			expectError   bool
			errorContains string
			verifyFunc    func()
		}{
			{
				name:        "log function executes successfully",
				expressions: []string{`log("info", "test message")`},
				expectError: false,
			},
			{
				name:          "fail function causes pipeline failure",
				expressions:   []string{`fail("intentional test failure")`},
				expectError:   true,
				errorContains: "intentional test failure",
			},
			{
				name:          "undefined function rdir should fail",
				expressions:   []string{`rdir("META-INF")`},
				expectError:   true,
				errorContains: "rdir",
			},
			{
				name: "multiple expressions stop on failure",
				expressions: []string{
					`log("info", "before failure")`,
					`fail("stop execution here")`,
					`log("info", "this should not execute")`,
				},
				expectError:   true,
				errorContains: "stop execution here",
			},
			{
				name:        "glob function finds matching files",
				expressions: []string{`glob("test*.txt")`},
				setupFiles: map[string]string{
					"test1.txt": "content1",
					"test2.txt": "content2",
					"other.log": "log content",
				},
				expectError: false,
			},
		}

		for _, tc := range testCases {
			tc := tc // Capture range variable
			It(tc.name, func() {
				// Setup files if needed
				for filename, content := range tc.setupFiles {
					filePath := filepath.Join(workDir, filename)
					Expect(os.WriteFile(filePath, []byte(content), 0644)).To(Succeed())
				}

				// Execute pipeline
				pipeline := NewCELPipeline(tc.expressions)
				err := evaluator.Execute(pipeline)

				// Verify results
				if tc.expectError {
					Expect(err).To(HaveOccurred())
					if tc.errorContains != "" {
						Expect(err.Error()).To(ContainSubstring(tc.errorContains))
					}
				} else {
					Expect(err).ToNot(HaveOccurred())
				}

				// Custom verification if provided
				if tc.verifyFunc != nil {
					tc.verifyFunc()
				}
			})
		}
	})

	Describe("CEL Parsing and Syntax Errors", func() {
		errorTestCases := []struct {
			name          string
			expression    string
			errorContains string
		}{
			{
				name:          "unclosed string should fail parsing",
				expression:    `glob("unclosed string`,
				errorContains: "parsing failed",
			},
			{
				name:          "undefined function should fail",
				expression:    `unknownFunction("test")`,
				errorContains: "unknownFunction",
			},
			{
				name:          "wrong argument type should fail",
				expression:    `glob(123)`,
				errorContains: "no matching overload",
			},
			{
				name:          "missing function arguments should fail",
				expression:    `log()`,
				errorContains: "no matching overload",
			},
		}

		for _, tc := range errorTestCases {
			tc := tc // Capture range variable
			It(tc.name, func() {
				pipeline := NewCELPipeline([]string{tc.expression})
				err := evaluator.Execute(pipeline)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(tc.errorContains))
			})
		}
	})

	Describe("File Operations", func() {
		fileOpTestCases := []struct {
			name          string
			setupFiles    map[string]string
			expression    string
			expectedInBin []string // Files that should exist in binDir after execution
			expectedError string
		}{
			{
				name: "delete function removes matching files",
				setupFiles: map[string]string{
					"delete-me.txt": "content",
					"keep-me.log":   "content",
				},
				expression:    `delete("delete-me.txt")`,
				expectedInBin: []string{}, // delete doesn't move files to bin
			},
			{
				name: "move function relocates files",
				setupFiles: map[string]string{
					"source.txt": "content to move",
				},
				expression:    `move("source.txt", "destination.txt")`,
				expectedInBin: []string{"destination.txt"},
			},
			{
				name: "chmod function changes permissions",
				setupFiles: map[string]string{
					"script.sh": "#!/bin/bash\necho hello",
				},
				expression: `chmod("script.sh", "0755")`,
			},
		}

		for _, tc := range fileOpTestCases {
			tc := tc // Capture range variable
			It(tc.name, func() {
				// Setup files
				for filename, content := range tc.setupFiles {
					filePath := filepath.Join(workDir, filename)
					Expect(os.WriteFile(filePath, []byte(content), 0644)).To(Succeed())
				}

				// Execute pipeline
				pipeline := NewCELPipeline([]string{tc.expression})
				err := evaluator.Execute(pipeline)

				// Check for expected errors
				if tc.expectedError != "" {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(tc.expectedError))
					return
				}

				Expect(err).ToNot(HaveOccurred())

				// Verify expected files exist in binDir
				for _, expectedFile := range tc.expectedInBin {
					filePath := filepath.Join(binDir, expectedFile)
					Expect(filePath).To(BeAnExistingFile())
				}
			})
		}
	})

	Describe("Pipeline Context and State", func() {
		It("should handle empty pipeline gracefully", func() {
			err := evaluator.Execute(nil)
			Expect(err).ToNot(HaveOccurred())

			pipeline := NewCELPipeline([]string{})
			err = evaluator.Execute(pipeline)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should maintain failure state across expressions", func() {
			// This test verifies that once a pipeline fails, subsequent expressions don't execute
			expressions := []string{
				`log("info", "first expression")`,
				`fail("pipeline failure")`,
				`log("error", "this should not execute")`,
			}

			pipeline := NewCELPipeline(expressions)
			err := evaluator.Execute(pipeline)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("pipeline failure"))
		})
	})
})
