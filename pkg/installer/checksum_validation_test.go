package installer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unsafe"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/flanksource/clicky/task"
	flanksourceContext "github.com/flanksource/commons/context"
	"github.com/flanksource/commons/logger"
	"github.com/flanksource/deps/pkg/config"
	"github.com/flanksource/deps/pkg/download"
)

type testContext struct {
	TempDir    string
	OldWD      string
	BinDir     string
	ConfigFile string
	Cleanup    func()
}

var _ = Describe("Checksum Validation", func() {
	var (
		testServer  *httptest.Server
		testCtx     *testContext
		serverPort  string
		testBinary  []byte
		correctHash string
	)

	BeforeEach(func() {
		// Create a small test binary content
		testBinary = []byte("#!/bin/bash\necho 'test binary'\n")

		// Calculate correct SHA256 hash of test binary
		hash := sha256.Sum256(testBinary)
		correctHash = fmt.Sprintf("%x", hash[:])

		// Create mock HTTP server
		testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/test-binary-darwin-arm64":
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(testBinary)

			case "/checksums":
				// Create checksums file with correct hash
				checksumContent := fmt.Sprintf("%s  test-binary-darwin-arm64\n", correctHash)
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(checksumContent))

			case "/checksums_hashes_order":
				// Create hash order file
				orderContent := "SHA-256\nMD5\n"
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(orderContent))

			default:
				http.NotFound(w, r)
			}
		}))

		// Extract port from server URL
		serverURL := testServer.URL
		parts := strings.Split(serverURL, ":")
		serverPort = parts[len(parts)-1]

		// Create temporary test environment
		var err error
		testCtx, err = createSimpleTestEnvironment()
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if testServer != nil {
			testServer.Close()
		}
		if testCtx != nil {
			testCtx.Cleanup()
		}
	})

	Context("With multiple checksum files and CEL expression", func() {
		It("should fail download when CEL expression returns incorrect checksum", func() {
			// Test the download package directly with multiple checksum URLs and CEL expression

			// Create URLs for multiple checksum files
			checksumURLs := []string{
				fmt.Sprintf("http://localhost:%s/checksums", serverPort),
				fmt.Sprintf("http://localhost:%s/checksums_hashes_order", serverPort),
			}
			checksumNames := []string{"checksums", "checksums_hashes_order"}
			// Use the correct hash but modify last character to make it wrong
			incorrectHash := correctHash[:len(correctHash)-1] + "x"
			checksumExpr := fmt.Sprintf("'sha256:%s'", incorrectHash)

			// Create test task
			testTask := &task.Task{}

			// Create temporary download destination
			tempFile := filepath.Join(testCtx.TempDir, "test-download")

			// Attempt download with bad checksum - this should fail
			downloadURL := fmt.Sprintf("http://localhost:%s/test-binary-darwin-arm64", serverPort)
			err := download.Download(downloadURL, tempFile, testTask,
				download.WithChecksumURLsAndNames(checksumURLs, checksumNames, checksumExpr))

			// Verify download failed with checksum error
			Expect(err).To(HaveOccurred(), "Download should fail due to checksum mismatch")
			Expect(err.Error()).To(ContainSubstring("checksum mismatch"),
				"Error should mention checksum mismatch")

			// Verify no file was created
			_, err = os.Stat(tempFile)
			Expect(os.IsNotExist(err)).To(BeTrue(),
				"File should not exist when checksum validation fails")
		})

		It("should provide access to both checksum variables in CEL expression", func() {
			// This test verifies that the CEL expression can access both
			// 'checksums' and 'checksums_hashes_order' variables
			// We use a CEL expression that depends on both variables but still returns incorrect checksum

			// Create URLs for multiple checksum files
			checksumURLs := []string{
				fmt.Sprintf("http://localhost:%s/checksums", serverPort),
				fmt.Sprintf("http://localhost:%s/checksums_hashes_order", serverPort),
			}
			checksumNames := []string{"checksums", "checksums_hashes_order"}
			// Use the correct hash but modify last character to make it wrong
			incorrectHash := correctHash[:len(correctHash)-1] + "x"
			checksumExpr := fmt.Sprintf("size(checksums) > 0 && size(checksums_hashes_order) > 0 ? 'sha256:%s' : 'sha256:0000000000000000000000000000000000000000000000000000000000000000'", incorrectHash)

			// Create test task
			testTask := &task.Task{}

			// Create temporary download destination
			tempFile := filepath.Join(testCtx.TempDir, "test-download-cel")

			// Attempt download
			downloadURL := fmt.Sprintf("http://localhost:%s/test-binary-darwin-arm64", serverPort)
			err := download.Download(downloadURL, tempFile, testTask,
				download.WithChecksumURLsAndNames(checksumURLs, checksumNames, checksumExpr))

			// Verify download failed (due to incorrect checksum)
			Expect(err).To(HaveOccurred(), "Download should fail due to checksum mismatch")

			// The key test: verify the error is checksum mismatch, not CEL evaluation error
			// This proves that the CEL expression successfully accessed both variables
			Expect(err.Error()).To(ContainSubstring("checksum mismatch"))
			Expect(err.Error()).ToNot(ContainSubstring("undeclared reference"))
			Expect(err.Error()).ToNot(ContainSubstring("variable"))
		})
	})

	Context("Info level logging", func() {
		It("should log checksum verification at Info level when installing jq from GitHub", func() {
			// Skip if no GitHub token (rate limiting)
			if os.Getenv("GITHUB_TOKEN") == "" {
				Skip("Skipping test - GITHUB_TOKEN not set (needed to avoid rate limiting)")
			}

			// Create temp directory for fresh installation
			tmpDir := GinkgoT().TempDir()
			binDir := filepath.Join(tmpDir, "bin")

			// Load jq package from default config
			depsConfig := config.GetGlobalRegistry()
			_, exists := depsConfig.Registry["jq"]
			Expect(exists).To(BeTrue(), "jq package should exist in default registry")

			// Create installer with temp directories
			inst := NewWithConfig(depsConfig,
				WithBinDir(binDir),
			)

			// Create task with Info level logging
			testLogger := logger.StandardLogger()
			testLogger.SetMinLogLevel(logger.Info)
			ctx := flanksourceContext.NewContext(context.Background(), flanksourceContext.WithLogger(testLogger))

			// Create task manually to get access to it
			testTask := &task.Task{}
			// Use reflection to initialize the task's context
			taskValue := reflect.ValueOf(testTask).Elem()
			ctxField := taskValue.FieldByName("ctx")
			if ctxField.IsValid() && ctxField.CanSet() {
				ctxField.Set(reflect.ValueOf(ctx))
			}

			// Install jq fresh (no lock file)
			err := inst.Install("jq", "1.8.0", testTask)
			Expect(err).ToNot(HaveOccurred(), "Installation should succeed")

			// Access buffered logs using reflection
			bufferedLoggerField := taskValue.FieldByName("bufferedLogger")
			Expect(bufferedLoggerField.IsValid()).To(BeTrue(), "bufferedLogger field should exist")

			// Get the interface value using unsafe pointer
			// This works because we're in a test binary
			var bufferedLogger *logger.BufferedLogger
			if bufferedLoggerField.CanInterface() {
				bufferedLogger, _ = bufferedLoggerField.Interface().(*logger.BufferedLogger)
			} else {
				// Use unsafe to access private field
				bufferedLogger = (*logger.BufferedLogger)(unsafe.Pointer(bufferedLoggerField.UnsafeAddr()))
			}

			Expect(bufferedLogger).ToNot(BeNil(), "BufferedLogger should not be nil")

			// Get logs
			logs := bufferedLogger.GetLogs()

			// Verify checksum log exists at Info level
			foundChecksumLog := false
			for _, log := range logs {
				GinkgoWriter.Printf("Log: [%s] %s\n", log.Level, log.Message)
				if log.Level == logger.Info && strings.Contains(log.Message, "✓ Checksum verified: SHA256:") {
					foundChecksumLog = true
					break
				}
			}

			Expect(foundChecksumLog).To(BeTrue(), "Expected to find checksum verification log at Info level")
		})
	})

	Context("GitHub GraphQL digest handling", func() {
		It("should correctly strip sha256: prefix from GitHub digest field", func() {
			// This test verifies that GitHub GraphQL digests (which come with sha256: prefix)
			// are properly stripped before being reassembled with the prefix

			// Create a mock server that mimics GitHub's behavior
			digestTestServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write(testBinary)
			}))
			defer digestTestServer.Close()

			// Extract port from server URL
			digestServerURL := digestTestServer.URL
			digestParts := strings.Split(digestServerURL, ":")
			digestServerPort := digestParts[len(digestParts)-1]

			// Create test task
			testTask := &task.Task{}

			// Create temporary download destination
			tempFile := filepath.Join(testCtx.TempDir, "test-digest-download")

			// Download with the correct checksum (GitHub format with sha256: prefix)
			// This simulates what happens when GitHub GraphQL returns the digest
			downloadURL := fmt.Sprintf("http://localhost:%s/test-binary", digestServerPort)
			githubStyleChecksum := fmt.Sprintf("sha256:%s", correctHash)

			err := download.Download(downloadURL, tempFile, testTask,
				download.WithChecksum(githubStyleChecksum))

			// Verify download succeeded
			Expect(err).ToNot(HaveOccurred(), "Download should succeed with correctly formatted checksum")

			// Verify file was created
			_, err = os.Stat(tempFile)
			Expect(err).ToNot(HaveOccurred(), "File should exist when checksum validation succeeds")
		})
	})
})

// createSimpleTestEnvironment sets up a simple test environment for direct download tests
func createSimpleTestEnvironment() (*testContext, error) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "deps-checksum-e2e-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Save current working directory
	oldWD, err := os.Getwd()
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	cleanup := func() {
		_ = os.Chdir(oldWD)
		os.RemoveAll(tempDir)
	}

	return &testContext{
		TempDir: tempDir,
		OldWD:   oldWD,
		Cleanup: cleanup,
	}, nil
}
