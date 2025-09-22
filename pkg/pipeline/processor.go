package pipeline

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/commons/files"
)

// Processor executes pipeline operations in a sandboxed environment
type Processor struct {
	workDir    string
	binDir     string
	debug      bool
	task       *task.Task
	sandboxDir string // Temporary directory for safe extraction
}

// NewProcessor creates a new pipeline processor
func NewProcessor(workDir, binDir string, t *task.Task, debug bool) *Processor {
	return &Processor{
		workDir: workDir,
		binDir:  binDir,
		debug:   debug,
		task:    t,
	}
}

// Execute runs the pipeline operations
func (p *Processor) Execute(pipeline *Pipeline) error {
	if pipeline == nil || len(pipeline.Operations) == 0 {
		return nil
	}

	// Create a sandbox directory for operations
	sandboxDir, err := os.MkdirTemp("", "deps-pipeline-")
	if err != nil {
		return fmt.Errorf("failed to create sandbox directory: %w", err)
	}
	p.sandboxDir = sandboxDir

	// Clean up sandbox unless debug mode
	if !p.debug {
		defer os.RemoveAll(sandboxDir)
	} else if p.task != nil {
		p.task.Infof("Debug mode: keeping sandbox directory %s", sandboxDir)
	}

	// Set the working directory for the pipeline
	pipeline.WorkDir = sandboxDir

	// Execute each operation in sequence
	for i, op := range pipeline.Operations {
		if p.task != nil {
			p.task.Debugf("Pipeline: executing operation %d: %s(%s)", i+1, op.Name, strings.Join(op.Args, ", "))
		}

		result, err := p.executeOperation(op)
		if err != nil {
			return fmt.Errorf("pipeline operation '%s' failed: %w", op.Name, err)
		}

		// Store result for potential use by next operations
		pipeline.Operations[i].Result = result
	}

	// Move final results to binDir
	return p.moveResults()
}

// executeOperation executes a single pipeline operation
func (p *Processor) executeOperation(op Operation) (interface{}, error) {
	opType := OperationType(op.Name)
	if !opType.IsValid() {
		return nil, fmt.Errorf("unknown operation: %s", op.Name)
	}

	switch opType {
	case OpUnarchive:
		return p.opUnarchive(op.Args)
	case OpChdir:
		return p.opChdir(op.Args)
	case OpGlob:
		return p.opGlob(op.Args)
	case OpCleanup:
		return p.opCleanup(op.Args)
	case OpMove:
		return p.opMove(op.Args)
	case OpDelete:
		return p.opDelete(op.Args)
	case OpChmod:
		return p.opChmod(op.Args)
	default:
		return nil, fmt.Errorf("unimplemented operation: %s", op.Name)
	}
}

// opUnarchive extracts archives matching the pattern
func (p *Processor) opUnarchive(args []string) (interface{}, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("unarchive requires a pattern argument")
	}

	pattern := p.evaluateArg(args[0])

	// Find files matching the pattern
	matches, err := filepath.Glob(filepath.Join(p.sandboxDir, pattern))
	if err != nil {
		return nil, fmt.Errorf("failed to glob pattern %s: %w", pattern, err)
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no files matching pattern: %s", pattern)
	}

	extracted := []string{}
	for _, match := range matches {
		if p.task != nil {
			p.task.Debugf("Pipeline: extracting %s", match)
		}

		// Determine extraction based on file extension
		lowerPath := strings.ToLower(match)
		var extractErr error

		switch {
		case strings.HasSuffix(lowerPath, ".tar.gz") || strings.HasSuffix(lowerPath, ".tgz"):
			extractErr = files.Untar(match, p.sandboxDir)
		case strings.HasSuffix(lowerPath, ".tar.xz") || strings.HasSuffix(lowerPath, ".txz"):
			extractErr = files.Untar(match, p.sandboxDir)
		case strings.HasSuffix(lowerPath, ".tar"):
			extractErr = files.Untar(match, p.sandboxDir)
		case strings.HasSuffix(lowerPath, ".zip") || strings.HasSuffix(lowerPath, ".jar"):
			extractErr = files.Unzip(match, p.sandboxDir)
		default:
			return nil, fmt.Errorf("unsupported archive format: %s", match)
		}

		if extractErr != nil {
			return nil, fmt.Errorf("failed to extract %s: %w", match, extractErr)
		}

		extracted = append(extracted, match)

		// Delete the archive after extraction
		if err := os.Remove(match); err != nil && p.debug {
			if p.task != nil {
				p.task.Debugf("Pipeline: failed to remove archive %s: %v", match, err)
			}
		}
	}

	return extracted, nil
}

// opChdir promotes a directory's contents to the root and removes everything else
func (p *Processor) opChdir(args []string) (interface{}, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("chdir requires a pattern argument")
	}

	pattern := p.evaluateArg(args[0])

	// Find directories matching the pattern
	entries, err := os.ReadDir(p.sandboxDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read sandbox directory: %w", err)
	}

	var targetDir string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		matched, err := filepath.Match(stripTypeSuffix(pattern), entry.Name())
		if err != nil {
			continue
		}

		if matched {
			targetDir = filepath.Join(p.sandboxDir, entry.Name())
			break
		}
	}

	if targetDir == "" {
		return nil, fmt.Errorf("no directory matching pattern: %s", pattern)
	}

	if p.task != nil {
		p.task.Debugf("Pipeline: promoting directory %s to root", targetDir)
	}

	// Create a temporary directory to hold the contents
	tempDir, err := os.MkdirTemp("", "chdir-temp-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Move target directory contents to temp
	targetEntries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read target directory: %w", err)
	}

	for _, entry := range targetEntries {
		src := filepath.Join(targetDir, entry.Name())
		dst := filepath.Join(tempDir, entry.Name())
		if err := os.Rename(src, dst); err != nil {
			return nil, fmt.Errorf("failed to move %s to temp: %w", src, err)
		}
	}

	// Remove everything in sandbox
	entries, err = os.ReadDir(p.sandboxDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read sandbox directory: %w", err)
	}

	for _, entry := range entries {
		path := filepath.Join(p.sandboxDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			if p.task != nil {
				p.task.Debugf("Pipeline: failed to remove %s: %v", path, err)
			}
		}
	}

	// Move everything from temp back to sandbox
	tempEntries, err := os.ReadDir(tempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp directory: %w", err)
	}

	for _, entry := range tempEntries {
		src := filepath.Join(tempDir, entry.Name())
		dst := filepath.Join(p.sandboxDir, entry.Name())
		if err := os.Rename(src, dst); err != nil {
			return nil, fmt.Errorf("failed to move %s to sandbox: %w", src, err)
		}
	}

	return targetDir, nil
}

// opGlob finds files/directories matching a pattern
func (p *Processor) opGlob(args []string) (interface{}, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("glob requires a pattern argument")
	}

	pattern := args[0]
	typeFilter := ""

	// Check for type suffix (e.g., "*:dir", "*:executable")
	if idx := strings.Index(pattern, ":"); idx > 0 {
		typeFilter = pattern[idx+1:]
		pattern = pattern[:idx]
	}

	matches, err := filepath.Glob(filepath.Join(p.sandboxDir, pattern))
	if err != nil {
		return nil, fmt.Errorf("failed to glob pattern %s: %w", pattern, err)
	}

	// Apply type filter if specified
	if typeFilter != "" {
		filtered := []string{}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				continue
			}

			switch typeFilter {
			case "dir":
				if info.IsDir() {
					filtered = append(filtered, match)
				}
			case "executable":
				if !info.IsDir() && info.Mode()&0111 != 0 {
					filtered = append(filtered, match)
				}
			case "archive":
				if isArchive(match) {
					filtered = append(filtered, match)
				}
			default:
				filtered = append(filtered, match)
			}
		}
		matches = filtered
	}

	// Return relative paths if a single match
	if len(matches) == 1 {
		rel, _ := filepath.Rel(p.sandboxDir, matches[0])
		return rel, nil
	}

	// Return relative paths for multiple matches
	results := []string{}
	for _, match := range matches {
		rel, _ := filepath.Rel(p.sandboxDir, match)
		results = append(results, rel)
	}
	return results, nil
}

// opCleanup removes temporary files and artifacts
func (p *Processor) opCleanup(args []string) (interface{}, error) {
	entries, err := os.ReadDir(p.sandboxDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read sandbox directory: %w", err)
	}

	cleaned := []string{}
	for _, entry := range entries {
		path := filepath.Join(p.sandboxDir, entry.Name())

		// Keep executables and certain file types
		if !entry.IsDir() {
			info, err := os.Stat(path)
			if err == nil {
				// Keep executable files
				if info.Mode()&0111 != 0 {
					continue
				}
				// Keep certain extensions
				ext := strings.ToLower(filepath.Ext(path))
				if ext == ".so" || ext == ".dll" || ext == ".dylib" {
					continue
				}
			}
		}

		// Remove temporary files and directories
		if isTemporaryFile(entry.Name()) {
			if err := os.RemoveAll(path); err != nil {
				if p.task != nil {
					p.task.Debugf("Pipeline: failed to clean up %s: %v", path, err)
				}
			} else {
				cleaned = append(cleaned, entry.Name())
			}
		}
	}

	return cleaned, nil
}

// opMove moves files from source to destination
func (p *Processor) opMove(args []string) (interface{}, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("move requires source and destination arguments")
	}

	src := p.evaluateArg(args[0])
	dst := p.evaluateArg(args[1])

	// Ensure paths are within sandbox
	srcPath := filepath.Join(p.sandboxDir, src)
	dstPath := filepath.Join(p.sandboxDir, dst)

	if err := validateSandboxPath(srcPath, p.sandboxDir); err != nil {
		return nil, fmt.Errorf("invalid source path: %w", err)
	}
	if err := validateSandboxPath(dstPath, p.sandboxDir); err != nil {
		return nil, fmt.Errorf("invalid destination path: %w", err)
	}

	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		return nil, fmt.Errorf("failed to move %s to %s: %w", src, dst, err)
	}

	return dstPath, nil
}

// opDelete deletes files matching a pattern
func (p *Processor) opDelete(args []string) (interface{}, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("delete requires a pattern argument")
	}

	pattern := p.evaluateArg(args[0])
	matches, err := filepath.Glob(filepath.Join(p.sandboxDir, pattern))
	if err != nil {
		return nil, fmt.Errorf("failed to glob pattern %s: %w", pattern, err)
	}

	deleted := []string{}
	for _, match := range matches {
		if err := validateSandboxPath(match, p.sandboxDir); err != nil {
			continue
		}

		if err := os.RemoveAll(match); err != nil {
			if p.task != nil {
				p.task.Debugf("Pipeline: failed to delete %s: %v", match, err)
			}
		} else {
			deleted = append(deleted, match)
		}
	}

	return deleted, nil
}

// opChmod changes permissions on files
func (p *Processor) opChmod(args []string) (interface{}, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("chmod requires pattern and mode arguments")
	}

	pattern := p.evaluateArg(args[0])
	modeStr := p.evaluateArg(args[1])

	// Parse mode (expecting octal like 0755)
	var mode os.FileMode
	if _, err := fmt.Sscanf(modeStr, "%o", &mode); err != nil {
		return nil, fmt.Errorf("invalid mode: %s", modeStr)
	}

	matches, err := filepath.Glob(filepath.Join(p.sandboxDir, pattern))
	if err != nil {
		return nil, fmt.Errorf("failed to glob pattern %s: %w", pattern, err)
	}

	changed := []string{}
	for _, match := range matches {
		if err := os.Chmod(match, mode); err != nil {
			if p.task != nil {
				p.task.Debugf("Pipeline: failed to chmod %s: %v", match, err)
			}
		} else {
			changed = append(changed, match)
		}
	}

	return changed, nil
}

// evaluateArg evaluates an argument which might be a nested function call
func (p *Processor) evaluateArg(arg string) string {
	arg = strings.TrimSpace(arg)

	// Remove quotes if present
	if (strings.HasPrefix(arg, "\"") && strings.HasSuffix(arg, "\"")) ||
		(strings.HasPrefix(arg, "'") && strings.HasSuffix(arg, "'")) {
		return arg[1 : len(arg)-1]
	}

	// Check if it's a function call like glob("*.txt")
	if idx := strings.Index(arg, "("); idx > 0 {
		funcName := arg[:idx]
		if funcName == "glob" {
			// Parse and execute the glob operation
			op, err := parseOperation(arg)
			if err == nil {
				result, err := p.executeOperation(op)
				if err == nil {
					switch v := result.(type) {
					case string:
						return v
					case []string:
						if len(v) > 0 {
							return v[0]
						}
					}
				}
			}
		}
	}

	return arg
}

// moveResults moves the final results from sandbox to binDir
func (p *Processor) moveResults() error {
	entries, err := os.ReadDir(p.sandboxDir)
	if err != nil {
		return fmt.Errorf("failed to read sandbox directory: %w", err)
	}

	for _, entry := range entries {
		src := filepath.Join(p.sandboxDir, entry.Name())
		dst := filepath.Join(p.binDir, entry.Name())

		// Skip non-executable files unless they're libraries
		if !entry.IsDir() {
			info, err := os.Stat(src)
			if err != nil {
				continue
			}

			// Check if it's executable or a library
			if info.Mode()&0111 == 0 {
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				if ext != ".so" && ext != ".dll" && ext != ".dylib" {
					continue
				}
			}
		}

		if p.task != nil {
			p.task.Debugf("Pipeline: moving %s to %s", src, dst)
		}

		// Remove destination if it exists
		os.RemoveAll(dst)

		// Move the file/directory
		if err := moveAll(src, dst); err != nil {
			return fmt.Errorf("failed to move %s to bin directory: %w", entry.Name(), err)
		}
	}

	return nil
}

// Helper functions

func isArchive(path string) bool {
	lower := strings.ToLower(path)
	extensions := []string{".tar", ".tar.gz", ".tgz", ".tar.xz", ".txz", ".zip", ".jar"}
	for _, ext := range extensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func isTemporaryFile(name string) bool {
	// Common temporary file patterns
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") {
		return true
	}
	if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".temp") {
		return true
	}
	// Common build artifacts
	if name == "LICENSE" || name == "README" || name == "README.md" ||
		name == "CHANGELOG" || name == "CHANGELOG.md" || name == "NOTICE" {
		return true
	}
	return false
}

func stripTypeSuffix(pattern string) string {
	if idx := strings.Index(pattern, ":"); idx > 0 {
		return pattern[:idx]
	}
	return pattern
}

func validateSandboxPath(path, sandbox string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absSandbox, err := filepath.Abs(sandbox)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absPath, absSandbox) {
		return fmt.Errorf("path %s is outside sandbox %s", path, sandbox)
	}
	return nil
}

func moveAll(src, dst string) error {
	// Try rename first (fastest if on same filesystem)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Fall back to copy and delete
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, info.Mode())
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if err := copyFile(srcPath, dstPath, info.Mode()); err != nil {
				return err
			}
		}
	}

	return os.RemoveAll(src)
}

func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	if err := os.Chmod(dst, mode); err != nil {
		return err
	}

	return os.Remove(src)
}