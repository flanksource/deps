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
				errorContains: "Syntax error",
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
			name              string
			setupFiles        map[string]string
			expression        string
			expectedInWorkDir []string // Files that should exist in workDir after execution
			expectedError     string
		}{
			{
				name: "delete function removes matching files",
				setupFiles: map[string]string{
					"delete-me.txt": "content",
					"keep-me.log":   "content",
				},
				expression:        `delete("delete-me.txt")`,
				expectedInWorkDir: []string{"keep-me.log"}, // delete doesn't keep files
			},
			{
				name: "delete with glob results removes multiple files",
				setupFiles: map[string]string{
					"file1.bat": "content",
					"file2.bat": "content",
					"keep.txt":  "content",
				},
				expression:        `delete(glob("*.bat"))`,
				expectedInWorkDir: []string{"keep.txt"}, // delete doesn't keep files
			},
			{
				name: "delete with glob single result",
				setupFiles: map[string]string{
					"dir1/a.test": "",
					"dir2/b.test": "",
					"file.txt":    "content",
				},
				expression:        `delete(glob("*:dir")[0])`,
				expectedInWorkDir: []string{"file.txt", "dir2/b.test"}, // delete doesn't keep files
			},
			{
				name: "move function relocates files",
				setupFiles: map[string]string{
					"source.txt": "content to move",
				},
				expression:        `move("source.txt", "destination.txt")`,
				expectedInWorkDir: []string{"destination.txt"},
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
					os.MkdirAll(filepath.Dir(filePath), 0755)
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

				// Verify expected files exist in workDir
				for _, expectedFile := range tc.expectedInWorkDir {
					filePath := filepath.Join(workDir, expectedFile)
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

	Describe("NewCELPipeline", func() {
		It("should return nil for empty expression list", func() {
			pipeline := NewCELPipeline([]string{})
			Expect(pipeline).To(BeNil())
		})

		It("should create pipeline with single expression", func() {
			pipeline := NewCELPipeline([]string{`glob("*.txt")`})
			Expect(pipeline).NotTo(BeNil())
			Expect(pipeline.RawExpression).To(Equal(`glob("*.txt")`))
			Expect(pipeline.Expressions).To(HaveLen(1))
			Expect(pipeline.Expressions[0]).To(Equal(`glob("*.txt")`))
		})

		It("should create pipeline with multiple expressions", func() {
			pipeline := NewCELPipeline([]string{`glob("*.txt")`, `log("info", "found files")`})
			Expect(pipeline).NotTo(BeNil())
			Expect(pipeline.Expressions).To(HaveLen(2))
			Expect(pipeline.Expressions[0]).To(Equal(`glob("*.txt")`))
			Expect(pipeline.Expressions[1]).To(Equal(`log("info", "found files")`))
			Expect(pipeline.RawExpression).To(Equal(`glob("*.txt"); log("info", "found files")`))
		})

		It("should trim whitespace from expressions", func() {
			pipeline := NewCELPipeline([]string{`  glob("*.txt")  `, `   log("info", "test")   `})
			Expect(pipeline).NotTo(BeNil())
			Expect(pipeline.Expressions).To(HaveLen(2))
			Expect(pipeline.Expressions[0]).To(Equal(`glob("*.txt")`))
			Expect(pipeline.Expressions[1]).To(Equal(`log("info", "test")`))
		})
	})

	Describe("CalculateDirectoryStats", func() {
		It("should return zero stats for empty directory", func() {
			tmpDir := GinkgoT().TempDir()
			fileCount, totalSize, err := calculateDirectoryStats(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(fileCount).To(Equal(0))
			Expect(totalSize).To(Equal(int64(0)))
		})

		It("should calculate stats for directory with files", func() {
			tmpDir := GinkgoT().TempDir()
			file1Content := []byte("Hello World")
			file2Content := []byte("Test content for stats")

			Expect(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), file1Content, 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tmpDir, "file2.txt"), file2Content, 0644))

			subDir := filepath.Join(tmpDir, "subdir")
			Expect(os.MkdirAll(subDir, 0755)).To(Succeed())
			file3Content := []byte("Nested file")
			Expect(os.WriteFile(filepath.Join(subDir, "file3.txt"), file3Content, 0644)).To(Succeed())

			fileCount, totalSize, err := calculateDirectoryStats(tmpDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(fileCount).To(Equal(3))
			expectedSize := int64(len(file1Content) + len(file2Content) + len(file3Content))
			Expect(totalSize).To(Equal(expectedSize))
		})

		It("should return error for nonexistent directory", func() {
			fileCount, totalSize, err := calculateDirectoryStats("/nonexistent/path")
			Expect(err).To(HaveOccurred())
			Expect(fileCount).To(Equal(0))
			Expect(totalSize).To(Equal(int64(0)))
		})
	})

	Describe("ListDirectoryFiles", func() {
		It("should return empty list for empty directory", func() {
			tmpDir := GinkgoT().TempDir()
			files := listDirectoryFiles(tmpDir)
			Expect(files).To(BeEmpty())
		})

		It("should list only files, not subdirectories", func() {
			tmpDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("nested"), 0644)).To(Succeed())

			files := listDirectoryFiles(tmpDir)
			Expect(files).To(HaveLen(2))
			Expect(files).To(ContainElement("file1.txt"))
			Expect(files).To(ContainElement("file2.txt"))
			Expect(files).NotTo(ContainElement("subdir"))
		})

		It("should return empty list for nonexistent directory", func() {
			files := listDirectoryFiles("/nonexistent/path")
			Expect(files).To(BeEmpty())
		})
	})

	Describe("FormatFileList", func() {
		It("should return 'none' for empty list", func() {
			result := formatFileList([]string{})
			Expect(result).To(Equal("none"))
		})

		It("should return single filename for single file", func() {
			result := formatFileList([]string{"file1.txt"})
			Expect(result).To(Equal("file1.txt"))
		})

		It("should return comma-separated list for few files", func() {
			files := []string{"file1.txt", "file2.txt", "file3.txt"}
			result := formatFileList(files)
			Expect(result).To(Equal("file1.txt, file2.txt, file3.txt"))
		})

		It("should truncate list for many files", func() {
			files := []string{"file1.txt", "file2.txt", "file3.txt", "file4.txt", "file5.txt", "file6.txt", "file7.txt"}
			result := formatFileList(files)
			Expect(result).To(Equal("file1.txt, file2.txt, file3.txt, file4.txt, file5.txt and 2 more"))
		})

		It("should not truncate for exactly max files", func() {
			files := []string{"file1.txt", "file2.txt", "file3.txt", "file4.txt", "file5.txt"}
			result := formatFileList(files)
			Expect(result).To(Equal("file1.txt, file2.txt, file3.txt, file4.txt, file5.txt"))
		})
	})

	Describe("ParseGlobPattern", func() {
		It("should parse pattern without type suffix", func() {
			pattern, typeFilter := parseGlobPattern("*.txt")
			Expect(pattern).To(Equal("*.txt"))
			Expect(typeFilter).To(Equal(""))
		})

		It("should parse pattern with dir type", func() {
			pattern, typeFilter := parseGlobPattern("*:dir")
			Expect(pattern).To(Equal("*"))
			Expect(typeFilter).To(Equal("dir"))
		})

		It("should parse pattern with exec type", func() {
			pattern, typeFilter := parseGlobPattern("sub*:exec")
			Expect(pattern).To(Equal("sub*"))
			Expect(typeFilter).To(Equal("exec"))
		})

		It("should parse pattern with archive type", func() {
			pattern, typeFilter := parseGlobPattern("*.tar.gz:archive")
			Expect(pattern).To(Equal("*.tar.gz"))
			Expect(typeFilter).To(Equal("archive"))
		})

		It("should handle pattern with multiple colons", func() {
			pattern, typeFilter := parseGlobPattern("file:with:colons:dir")
			Expect(pattern).To(Equal("file:with:colons"))
			Expect(typeFilter).To(Equal("dir"))
		})

		It("should handle pattern with empty type", func() {
			pattern, typeFilter := parseGlobPattern("*.txt:")
			Expect(pattern).To(Equal("*.txt"))
			Expect(typeFilter).To(Equal(""))
		})
	})

	Describe("ListDirectoryItems", func() {
		It("should list files only", func() {
			tmpDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tmpDir, "file2.py"), []byte("content"), 0644)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(tmpDir, "subdir1"), 0755)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(tmpDir, "subdir2"), 0755)).To(Succeed())

			items := listDirectoryItems(tmpDir, "files")
			Expect(items).To(HaveLen(2))
			Expect(items).To(ContainElement("file1.txt"))
			Expect(items).To(ContainElement("file2.py"))
			Expect(items).NotTo(ContainElement("subdir1"))
			Expect(items).NotTo(ContainElement("subdir2"))
		})

		It("should list directories only", func() {
			tmpDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tmpDir, "file2.py"), []byte("content"), 0644)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(tmpDir, "subdir1"), 0755)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(tmpDir, "subdir2"), 0755)).To(Succeed())

			items := listDirectoryItems(tmpDir, "dirs")
			Expect(items).To(HaveLen(2))
			Expect(items).To(ContainElement("subdir1"))
			Expect(items).To(ContainElement("subdir2"))
			Expect(items).NotTo(ContainElement("file1.txt"))
			Expect(items).NotTo(ContainElement("file2.py"))
		})

		It("should list all items", func() {
			tmpDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(tmpDir, "subdir1"), 0755)).To(Succeed())

			items := listDirectoryItems(tmpDir, "all")
			Expect(items).To(HaveLen(2))
			Expect(items).To(ContainElement("file1.txt"))
			Expect(items).To(ContainElement("subdir1"))
		})

		It("should default to files only for unknown filter", func() {
			tmpDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(tmpDir, "subdir1"), 0755)).To(Succeed())

			items := listDirectoryItems(tmpDir, "unknown")
			Expect(items).To(HaveLen(1))
			Expect(items).To(ContainElement("file1.txt"))
			Expect(items).NotTo(ContainElement("subdir1"))
		})

		It("should return empty list for nonexistent directory", func() {
			items := listDirectoryItems("/nonexistent/path", "files")
			Expect(items).To(BeEmpty())
		})
	})
})
