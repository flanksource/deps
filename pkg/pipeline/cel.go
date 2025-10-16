package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flanksource/commons/files"
	"github.com/flanksource/commons/text"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// CELPipelineEnvironment manages CEL evaluation for pipeline expressions
type CELPipelineEnvironment struct {
	ctx *PipelineContext
	env *cel.Env
}

// NewCELPipelineEnvironment creates a new CEL environment with pipeline functions
func NewCELPipelineEnvironment(ctx *PipelineContext) (*CELPipelineEnvironment, error) {
	f := &functions{ctx: ctx}

	// Create CEL environment with custom functions
	env, err := cel.NewEnv(
		// Register pipeline functions
		cel.Function("glob",
			cel.Overload("glob_string", []*cel.Type{cel.StringType}, cel.ListType(cel.StringType),
				cel.UnaryBinding(f.globCEL))),
		cel.Function("unarchive",
			cel.Overload("unarchive_list", []*cel.Type{cel.StringType}, cel.IntType,
				cel.UnaryBinding(f.unarchiveCEL))),
		cel.Function("move",
			cel.Overload("move_strings", []*cel.Type{cel.StringType, cel.StringType}, cel.StringType,
				cel.BinaryBinding(f.moveCEL))),
		cel.Function("chdir",
			cel.Overload("chdir_string", []*cel.Type{cel.StringType}, cel.StringType,
				cel.UnaryBinding(f.chdirCEL))),
		cel.Function("chmod",
			cel.Overload("chmod_strings", []*cel.Type{cel.StringType, cel.StringType}, cel.ListType(cel.StringType),
				cel.BinaryBinding(f.chmodCEL))),
		cel.Function("delete",
			cel.Overload("delete_string", []*cel.Type{cel.StringType}, cel.ListType(cel.StringType),
				cel.UnaryBinding(f.deleteCEL)),
			cel.Overload("delete_list", []*cel.Type{cel.ListType(cel.StringType)}, cel.ListType(cel.StringType),
				cel.UnaryBinding(f.deleteCEL))),
		cel.Function("cleanup",
			cel.Overload("cleanup_void", []*cel.Type{}, cel.ListType(cel.StringType),
				cel.FunctionBinding(f.cleanupCEL))),
		cel.Function("log",
			cel.Overload("log_strings", []*cel.Type{cel.StringType, cel.StringType}, cel.BoolType,
				cel.BinaryBinding(f.logCEL))),
		cel.Function("fail",
			cel.Overload("fail_string", []*cel.Type{cel.StringType}, cel.BoolType,
				cel.UnaryBinding(f.failCEL))),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}

	return &CELPipelineEnvironment{
		ctx: ctx,
		env: env,
	}, nil
}

// Evaluate evaluates a CEL expression
func (env *CELPipelineEnvironment) Evaluate(expression string) (interface{}, error) {
	if env.ctx.CheckFailed() {
		return nil, fmt.Errorf("pipeline already failed: %s", env.ctx.GetFailureMessage())
	}

	// Parse the CEL expression
	ast, issues := env.env.Parse(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}

	// Check the expression
	checked, issues := env.env.Check(ast)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}

	// Plan the expression
	program, err := env.env.Program(checked)
	if err != nil {
		return nil, err
	}

	// Evaluate the expression with empty input
	result, _, err := program.Eval(map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	// Check if pipeline failed during this expression evaluation
	if env.ctx.CheckFailed() {
		return nil, fmt.Errorf("%s", env.ctx.GetFailureMessage())
	}

	// Convert CEL result to Go value
	goValue := convertCELToGo(result)
	return goValue, nil
}

// convertCELToGo converts CEL ref.Val types to native Go types
func convertCELToGo(val ref.Val) interface{} {
	switch v := val.(type) {
	case types.String:
		return string(v)
	case types.Bool:
		return bool(v)
	case types.Int:
		return int64(v)
	case types.Double:
		return float64(v)
	case traits.Lister:
		// Convert CEL list to Go slice
		size := int(v.Size().Value().(int64))
		result := make([]interface{}, size)
		for i := 0; i < size; i++ {
			item := v.Get(types.Int(i))
			result[i] = convertCELToGo(item)
		}
		return result
	default:
		// Fallback to CEL's native conversion
		return val.Value()
	}
}

// functions implements CEL function definitions
type functions struct {
	ctx  *PipelineContext
	args []interface{}
}

// Logging helper methods for standardized function entry/exit patterns

// logFunctionEntry logs the start of a CEL function execution
func (f *functions) logFunctionEntry(funcName string, args ...interface{}) time.Time {
	start := time.Now()
	f.args = args
	if f.ctx.Task != nil {
		f.ctx.Task.V(3).Infof(fmt.Sprintf("%s(%v)", funcName, args))
	}
	return start
}

// logFunctionSuccess logs successful completion of a CEL function
func (f *functions) logFunctionSuccess(funcName string, start time.Time, result interface{}) {
	duration := time.Since(start)
	if f.ctx.Task != nil {
		f.ctx.Task.V(2).Infof(fmt.Sprintf("%s(%v) => %v (%s) ", funcName, f.args, f.formatResult(result), duration))
	}
}

// logFunctionError logs failed execution of a CEL function
func (f *functions) logFunctionError(funcName string, start time.Time, err error) {
	duration := time.Since(start)
	f.ctx.LogError(fmt.Sprintf("CEL %s: failed after %v with error: %v", funcName, duration, err))
}

// formatResult formats result for logging based on type
func (f *functions) formatResult(result interface{}) string {
	if result == nil {
		return ""
	}
	return fmt.Sprintf("%v", result)
}

// glob finds files matching a pattern with optional type filtering
func (f *functions) glob(pattern string, typeFilter ...string) ([]string, error) {
	start := f.logFunctionEntry("glob", pattern, typeFilter)

	// Parse pattern for type suffix (e.g., "*:dir", "*.txt:exec")
	actualPattern, typeFromPattern := parseGlobPattern(pattern)

	// Determine final filter: pattern suffix takes precedence over parameter
	filter := typeFromPattern
	if filter == "" && len(typeFilter) > 0 {
		filter = typeFilter[0]
	}

	absSandboxDir, _ := filepath.Abs(f.ctx.SandboxDir)
	// Find files matching the pattern
	searchPath := filepath.Join(f.ctx.SandboxDir, actualPattern)
	absSearchPath, _ := filepath.Abs(searchPath)
	f.ctx.LogDebug(fmt.Sprintf("Globbing absolute path: %s", absSearchPath))

	matches, err := filepath.Glob(searchPath)
	if err != nil {
		f.logFunctionError("glob", start, err)
		globErr := fmt.Errorf("glob failed: %v", err)
		f.ctx.FailPipeline(globErr.Error())
		return nil, globErr
	}

	f.ctx.LogDebug(fmt.Sprintf("Raw glob matches: %d files found in destination %s", len(matches), absSandboxDir))

	if len(matches) == 0 {
		// List appropriate items to help with debugging based on filter type
		var availableItems []string
		var itemType string

		switch filter {
		case "dir":
			availableItems = listDirectoryItems(f.ctx.SandboxDir, "dirs")
			itemType = "directories"
		default:
			// For "executable", "archive", or no filter, show files
			availableItems = listDirectoryItems(f.ctx.SandboxDir, "files")
			itemType = "files"
		}

		var errMsg string
		if filter != "" {
			errMsg = fmt.Sprintf("no files matching pattern: %s with filter %s in destination %s (available %s: [%s])",
				actualPattern, filter, absSandboxDir, itemType, formatFileList(availableItems))
		} else {
			errMsg = fmt.Sprintf("no files matching pattern: %s in destination %s (available %s: [%s])",
				actualPattern, absSandboxDir, itemType, formatFileList(availableItems))
		}

		err := fmt.Errorf("%s", errMsg)
		f.logFunctionError("glob", start, err)
		f.ctx.FailPipeline(err.Error())
		return nil, err
	}

	// Apply type filter if specified
	filtered := f.applyTypeFilter(matches, filter)
	if len(filtered) == 0 {
		// Show what files matched before filtering to help with debugging
		var matchedBeforeFilter []string
		for _, match := range matches {
			if rel, err := filepath.Rel(f.ctx.SandboxDir, match); err == nil {
				matchedBeforeFilter = append(matchedBeforeFilter, rel)
			} else {
				matchedBeforeFilter = append(matchedBeforeFilter, filepath.Base(match))
			}
		}
		err := fmt.Errorf("no files matching pattern %s with filter %s (matched %d files before filter: [%s])",
			actualPattern, filter, len(matches), formatFileList(matchedBeforeFilter))
		f.logFunctionError("glob", start, err)
		f.ctx.FailPipeline(err.Error())
		return nil, err
	}

	// Convert to relative paths
	var relatives []string
	for _, match := range filtered {
		rel, err := filepath.Rel(f.ctx.SandboxDir, match)
		if err != nil {
			f.ctx.LogDebug(fmt.Sprintf("Warning: could not get relative path for %s: %v", match, err))
			rel = filepath.Base(match) // fallback to basename
		}
		relatives = append(relatives, rel)
	}

	f.ctx.LogInfo(fmt.Sprintf("Found %d files matching pattern %s in destination %s: [%s]",
		len(relatives), pattern, absSandboxDir, formatFileList(relatives)))
	f.logFunctionSuccess("glob", start, relatives)

	return relatives, nil
}

// unarchive extracts archives
func (f *functions) unarchive(filename string) (int, error) {
	_ = f.logFunctionEntry("unarchive", filename)

	absSandboxDir, _ := filepath.Abs(f.ctx.SandboxDir)
	fullPath := filepath.Join(f.ctx.SandboxDir, filename)
	absFullPath, _ := filepath.Abs(fullPath)

	// Check if file exists and get size for logging
	if fileInfo, err := os.Stat(fullPath); err != nil {
		archiveErr := fmt.Errorf("archive file not found: %s", filename)
		handleFunctionError(f.ctx, "unarchive", archiveErr)
		return 0, archiveErr
	} else {
		f.ctx.LogInfo(fmt.Sprintf("Extracting %s (%s)", filepath.Base(filename), text.HumanizeBytes(fileInfo.Size())))
	}

	// Track extraction timing
	extractStart := time.Now()

	// Extract archive using files.Unarchive
	extractedFiles, err := files.Unarchive(fullPath, f.ctx.SandboxDir)
	if err != nil {
		extractErr := fmt.Errorf("failed to extract %s from %s to destination %s: %v", filename, absFullPath, absSandboxDir, err)
		f.ctx.FailPipeline(extractErr.Error())
		return 0, extractErr
	}

	extracted := []string{}
	for _, file := range extractedFiles.Files {
		rel, _ := filepath.Rel(f.ctx.SandboxDir, file)
		extracted = append(extracted, rel)
	}
	extractDuration := time.Since(extractStart)
	f.ctx.LogInfo(fmt.Sprintf("Extracted %s to destination %s in %v (%d files extracted)",
		filename, absSandboxDir, extractDuration, len(extractedFiles.Files)))
	if f.ctx.Task != nil {
		f.ctx.Task.V(4).Infof(fmt.Sprintf("Extracted files from %s to destination %s: %v", filename, absSandboxDir, extractedFiles.Files))
	}

	// Remove the archive after extraction
	if err := os.Remove(fullPath); err != nil {
		f.ctx.LogDebug(fmt.Sprintf("Failed to remove archive %s after extraction: %v", filename, err))
	} else {
		if f.ctx.Task != nil {
			f.ctx.Task.V(3).Infof(fmt.Sprintf("Removed archive %s after extraction", filename))
		}
	}

	// Calculate directory stats for enhanced success message
	fileCount, totalSize, err := calculateDirectoryStats(f.ctx.SandboxDir)
	if err != nil {
		return 0, err
	} else {
		f.ctx.LogInfo(fmt.Sprintf("Extracted %s (%d files, %s total)",
			filepath.Base(filename), fileCount, text.HumanizeBytes(totalSize)))
	}
	return len(extracted), nil
}

// calculateDirectoryStats calculates the total number of files and their combined size in a directory
func calculateDirectoryStats(dirPath string) (int, int64, error) {
	var fileCount int
	var totalSize int64

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories, only count files
		if !info.IsDir() {
			fileCount++
			totalSize += info.Size()
		}

		return nil
	})

	return fileCount, totalSize, err
}

// listDirectoryFiles returns a list of all files in the given directory (non-recursively)
func listDirectoryFiles(dirPath string) []string {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return []string{} // Return empty slice if directory can't be read
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}

	return files
}

// formatFileList formats a list of files for display in log messages
func formatFileList(files []string) string {
	if len(files) == 0 {
		return "none"
	}

	const maxFiles = 5
	if len(files) <= maxFiles {
		return strings.Join(files, ", ")
	}

	// Show first 5 files and indicate there are more
	shown := strings.Join(files[:maxFiles], ", ")
	remaining := len(files) - maxFiles
	return fmt.Sprintf("%s and %d more", shown, remaining)
}

// parseGlobPattern parses patterns with optional type suffix (e.g., "*:dir", "*.txt:exec")
func parseGlobPattern(pattern string) (string, string) {
	if strings.Contains(pattern, ":") {
		parts := strings.Split(pattern, ":")
		if len(parts) >= 2 {
			// Join all parts except the last as the pattern, last part is the type
			actualPattern := strings.Join(parts[:len(parts)-1], ":")
			typeFilter := parts[len(parts)-1]
			return actualPattern, typeFilter
		}
	}
	return pattern, "" // no type specified
}

// listDirectoryItems returns files, directories, or both based on itemType
func listDirectoryItems(dirPath string, itemType string) []string {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return []string{} // Return empty slice if directory can't be read
	}

	var items []string
	for _, entry := range entries {
		switch itemType {
		case "files":
			if !entry.IsDir() {
				items = append(items, entry.Name())
			}
		case "dirs":
			if entry.IsDir() {
				items = append(items, entry.Name())
			}
		case "all":
			items = append(items, entry.Name())
		default:
			// Default to files for backward compatibility
			if !entry.IsDir() {
				items = append(items, entry.Name())
			}
		}
	}

	return items
}

// move moves a file or directory
func (f *functions) move(src, dst string) (string, error) {
	start := f.logFunctionEntry("move", src, dst)

	absSandboxDir, _ := filepath.Abs(f.ctx.SandboxDir)
	f.ctx.LogInfo(fmt.Sprintf("Moving %s to %s within destination %s", src, dst, absSandboxDir))

	// Ensure paths are within destination directory
	srcPath := filepath.Join(f.ctx.SandboxDir, src)
	dstPath := filepath.Join(f.ctx.SandboxDir, dst)

	absSrcPath, _ := filepath.Abs(srcPath)
	absDstPath, _ := filepath.Abs(dstPath)
	f.ctx.LogDebug(fmt.Sprintf("Absolute destination paths: src=%s, dst=%s", absSrcPath, absDstPath))

	// Check if source exists
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		moveErr := fmt.Errorf("source path does not exist: %s", src)
		f.logFunctionError("move", start, moveErr)
		f.ctx.FailPipeline(moveErr.Error())
		return "", moveErr
	}

	srcType := "file"
	srcSize := srcInfo.Size()
	if srcInfo.IsDir() {
		srcType = "directory"
		// For directories, calculate number of entries
		if entries, err := os.ReadDir(srcPath); err == nil {
			srcSize = int64(len(entries))
			f.ctx.LogDebug(fmt.Sprintf("Source directory contains %d entries", len(entries)))
		}
	} else {
		f.ctx.LogDebug(fmt.Sprintf("Source file size: %d bytes", srcSize))
	}

	// Create destination directory if needed
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		moveErr := fmt.Errorf("failed to create destination directory: %v", err)
		f.logFunctionError("move", start, moveErr)
		f.ctx.FailPipeline(moveErr.Error())
		return "", moveErr
	}

	// Check if destination already exists
	if _, err := os.Stat(dstPath); err == nil {
		f.ctx.LogDebug(fmt.Sprintf("Destination %s already exists, will be overwritten", dst))
	}

	// Track move timing
	moveStart := time.Now()

	// Move the file/directory
	if err := os.Rename(srcPath, dstPath); err != nil {
		moveErr := fmt.Errorf("failed to move %s to %s: %v", src, dst, err)
		f.logFunctionError("move", start, moveErr)
		f.ctx.FailPipeline(moveErr.Error())
		return "", moveErr
	}

	moveDuration := time.Since(moveStart)
	f.ctx.LogInfo(fmt.Sprintf("Successfully moved %s (%s) from %s to %s within destination %s in %v",
		srcType, src, absSrcPath, absDstPath, absSandboxDir, moveDuration))

	f.logFunctionSuccess("move", start, dst)
	return dst, nil
}

// chdir changes to a directory (promotes its contents to root level)
func (f *functions) chdir(pattern string) (string, error) {
	start := f.logFunctionEntry("chdir", pattern)

	absSandboxDir, _ := filepath.Abs(f.ctx.SandboxDir)
	f.ctx.LogInfo(fmt.Sprintf("Looking for directory matching pattern: %s in destination %s", pattern, absSandboxDir))

	// Find directories matching the pattern
	f.ctx.LogDebug(fmt.Sprintf("Reading destination directory: %s", absSandboxDir))
	entries, err := os.ReadDir(f.ctx.SandboxDir)
	if err != nil {
		chdirErr := fmt.Errorf("failed to read destination directory %s: %v", absSandboxDir, err)
		f.logFunctionError("chdir", start, chdirErr)
		f.ctx.FailPipeline(chdirErr.Error())
		return "", chdirErr
	}

	f.ctx.LogDebug(fmt.Sprintf("Found %d entries in destination %s, searching for directories", len(entries), absSandboxDir))

	var targetDir string
	var dirCount int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirCount++

		f.ctx.LogDebug(fmt.Sprintf("Checking directory: %s against pattern: %s", entry.Name(), pattern))
		matched, err := filepath.Match(pattern, entry.Name())
		if err != nil {
			f.ctx.LogDebug(fmt.Sprintf("Pattern match error for %s: %v", entry.Name(), err))
			continue
		}

		if matched {
			targetDir = filepath.Join(f.ctx.SandboxDir, entry.Name())
			f.ctx.LogDebug(fmt.Sprintf("Found matching directory: %s", entry.Name()))
			break
		}
	}

	f.ctx.LogDebug(fmt.Sprintf("Examined %d directories in destination %s", dirCount, absSandboxDir))

	if targetDir == "" {
		chdirErr := fmt.Errorf("no directory matching pattern: %s in destination %s", pattern, absSandboxDir)
		f.logFunctionError("chdir", start, chdirErr)
		f.ctx.FailPipeline(chdirErr.Error())
		return "", chdirErr
	}

	// Check if directory is empty
	targetEntries, err := os.ReadDir(targetDir)
	if err != nil {
		chdirErr := fmt.Errorf("failed to read target directory: %v", err)
		f.logFunctionError("chdir", start, chdirErr)
		f.ctx.FailPipeline(chdirErr.Error())
		return "", chdirErr
	}

	if len(targetEntries) == 0 {
		chdirErr := fmt.Errorf("directory is empty: %s", filepath.Base(targetDir))
		f.logFunctionError("chdir", start, chdirErr)
		f.ctx.FailPipeline(chdirErr.Error())
		return "", chdirErr
	}

	dirName := filepath.Base(targetDir)
	absTargetDir, _ := filepath.Abs(targetDir)
	f.ctx.LogInfo(fmt.Sprintf("Promoting directory %s contents (%d entries) to root level in destination %s",
		dirName, len(targetEntries), absSandboxDir))

	// Track promotion timing
	promoteStart := time.Now()

	// Perform the directory promotion (implementation details in helper function)
	err = f.promoteDirectoryContents(targetDir)
	if err != nil {
		chdirErr := fmt.Errorf("failed to promote directory contents from %s: %v", absTargetDir, err)
		f.logFunctionError("chdir", start, chdirErr)
		f.ctx.FailPipeline(chdirErr.Error())
		return "", chdirErr
	}

	promoteDuration := time.Since(promoteStart)
	f.ctx.LogInfo(fmt.Sprintf("Successfully promoted directory %s contents from %s to destination %s in %v",
		dirName, absTargetDir, absSandboxDir, promoteDuration))

	f.logFunctionSuccess("chdir", start, dirName)
	return dirName, nil
}

// chmod changes file permissions
func (f *functions) chmod(pattern, modeStr string) ([]string, error) {
	start := f.logFunctionEntry("chmod", pattern, modeStr)

	absSandboxDir, _ := filepath.Abs(f.ctx.SandboxDir)
	f.ctx.LogInfo(fmt.Sprintf("Changing permissions of files matching %s to %s in destination %s", pattern, modeStr, absSandboxDir))

	// Parse mode (expecting octal like "0755")
	var mode os.FileMode
	if _, err := fmt.Sscanf(modeStr, "%o", &mode); err != nil {
		chmodErr := fmt.Errorf("invalid mode: %s", modeStr)
		f.logFunctionError("chmod", start, chmodErr)
		f.ctx.FailPipeline(chmodErr.Error())
		return nil, chmodErr
	}
	searchPath := filepath.Join(f.ctx.SandboxDir, pattern)
	absSearchPath, _ := filepath.Abs(searchPath)
	f.ctx.LogDebug(fmt.Sprintf("Searching for files at absolute path: %s", absSearchPath))
	matches, err := filepath.Glob(searchPath)
	if err != nil {
		chmodErr := fmt.Errorf("failed to glob pattern %s: %v", pattern, err)
		f.logFunctionError("chmod", start, chmodErr)
		f.ctx.FailPipeline(chmodErr.Error())
		return nil, chmodErr
	}

	f.ctx.LogDebug(fmt.Sprintf("Found %d files matching pattern %s in destination %s", len(matches), pattern, absSandboxDir))

	if len(matches) == 0 {
		chmodErr := fmt.Errorf("no files matching pattern: %s in destination %s", pattern, absSandboxDir)
		f.logFunctionError("chmod", start, chmodErr)
		f.ctx.FailPipeline(chmodErr.Error())
		return nil, chmodErr
	}

	var changed []string
	var failed []string
	for i, match := range matches {
		f.ctx.LogDebug(fmt.Sprintf("Changing permissions for file %d/%d: %s", i+1, len(matches), filepath.Base(match)))

		if err := os.Chmod(match, mode); err != nil {
			failed = append(failed, filepath.Base(match))
			f.ctx.LogDebug(fmt.Sprintf("Failed to chmod %s: %v", filepath.Base(match), err))
		} else {
			rel, err := filepath.Rel(f.ctx.SandboxDir, match)
			if err != nil {
				rel = filepath.Base(match) // fallback
			}
			changed = append(changed, rel)
			f.ctx.LogDebug(fmt.Sprintf("Changed permissions of %s to %s", rel, modeStr))
		}
	}

	if len(failed) > 0 {
		f.ctx.LogDebug(fmt.Sprintf("Failed to change permissions for %d files: %v", len(failed), failed))
	}

	f.logFunctionSuccess("chmod", start, changed)
	return changed, nil
}

// deleteFiles performs the actual deletion of files/directories
func (f *functions) deleteFiles(matches []string, context string) ([]string, error) {
	if len(matches) == 0 {
		return []string{}, nil
	}
	var deleted []string
	var failed []string
	var totalSize int64

	for _, match := range matches {
		// Ensure path is absolute and within sandbox
		var absMatch string
		if filepath.IsAbs(match) {
			absMatch = match
		} else {
			absMatch = filepath.Join(f.ctx.SandboxDir, match)
		}

		itemName := filepath.Base(absMatch)

		// Get size info before deletion for logging
		if info, err := os.Stat(absMatch); err == nil {
			if info.IsDir() {
				if entries, err := os.ReadDir(absMatch); err == nil {
					f.ctx.Task.V(4).Infof(fmt.Sprintf("Deleting directory '%s'  (%d entries)", itemName, len(entries)))
				}
			} else {
				totalSize += info.Size()
				f.ctx.Task.V(4).Infof(fmt.Sprintf("Deleting '%s' (%d bytes)", itemName, info.Size()))
			}
		}

		if err := os.RemoveAll(absMatch); err != nil {
			failed = append(failed, itemName)
			f.ctx.LogDebug(fmt.Sprintf("Failed to delete %s: %v", itemName, err))
		} else {
			rel, err := filepath.Rel(f.ctx.SandboxDir, absMatch)
			if err != nil {
				rel = itemName // fallback
			}
			deleted = append(deleted, rel)
		}
	}

	if len(failed) > 0 {
		f.ctx.LogError(fmt.Sprintf("Failed to delete %d items: %v", len(failed), failed))
	}

	if totalSize > 0 {
		f.ctx.LogDebug(fmt.Sprintf("Successfully deleted %d/%d items (freed %d bytes)",
			len(deleted), len(matches), totalSize))
	} else {
		f.ctx.LogDebug(fmt.Sprintf("Successfully deleted %d/%d items", len(deleted), len(matches)))
	}

	return deleted, nil
}

// delete removes files matching a pattern
func (f *functions) delete(pattern string) ([]string, error) {
	start := f.logFunctionEntry("delete", pattern)

	searchPath := filepath.Join(f.ctx.SandboxDir, pattern)
	absSearchPath, _ := filepath.Abs(searchPath)
	f.ctx.LogDebug(fmt.Sprintf("Deleting %s/%s ", pattern, absSearchPath))

	matches, err := filepath.Glob(searchPath)
	if err != nil {
		deleteErr := fmt.Errorf("failed to glob pattern %s: %v", pattern, err)
		f.logFunctionError("delete", start, deleteErr)
		f.ctx.FailPipeline(deleteErr.Error())
		return nil, deleteErr
	}

	if len(matches) == 0 {
		f.ctx.LogDebug(fmt.Sprintf("No files found matching pattern %s, nothing to delete", pattern))
		f.logFunctionSuccess("delete", start, []string{})
		return []string{}, nil
	}

	deleted, err := f.deleteFiles(matches, fmt.Sprintf("pattern '%s'", pattern))
	if err != nil {
		f.logFunctionError("delete", start, err)
		return nil, err
	}

	f.logFunctionSuccess("delete", start, deleted)
	return deleted, nil
}

// deleteList removes files from a list of paths
func (f *functions) deleteList(paths []string) ([]string, error) {
	start := f.logFunctionEntry("delete", fmt.Sprintf("list of %d paths", len(paths)))

	if len(paths) == 0 {
		f.ctx.LogDebug("Empty path list provided, nothing to delete")
		f.logFunctionSuccess("delete", start, []string{})
		return []string{}, nil
	}

	deleted, err := f.deleteFiles(paths, fmt.Sprintf("list of %d paths", len(paths)))
	if err != nil {
		f.logFunctionError("delete", start, err)
		return nil, err
	}

	f.logFunctionSuccess("delete", start, deleted)
	return deleted, nil
}

// cleanup removes temporary files and artifacts
func (f *functions) cleanup() ([]string, error) {
	start := f.logFunctionEntry("cleanup")

	absSandboxDir, _ := filepath.Abs(f.ctx.SandboxDir)
	f.ctx.LogInfo(fmt.Sprintf("Starting cleanup of temporary files and artifacts in destination %s", absSandboxDir))

	entries, err := os.ReadDir(f.ctx.SandboxDir)
	if err != nil {
		cleanupErr := fmt.Errorf("failed to read destination directory %s: %v", absSandboxDir, err)
		f.logFunctionError("cleanup", start, cleanupErr)
		f.ctx.FailPipeline(cleanupErr.Error())
		return nil, cleanupErr
	}

	f.ctx.LogDebug(fmt.Sprintf("Examining %d entries in destination %s for cleanup", len(entries), absSandboxDir))

	var cleaned []string
	var skipped []string
	var failed []string
	var totalSize int64

	for i, entry := range entries {
		entryName := entry.Name()
		f.ctx.LogDebug(fmt.Sprintf("Examining entry %d/%d: %s", i+1, len(entries), entryName))

		if f.isTemporaryFile(entryName) {
			path := filepath.Join(f.ctx.SandboxDir, entryName)

			// Get size before deletion for logging
			if info, err := os.Stat(path); err == nil {
				if info.IsDir() {
					if dirEntries, err := os.ReadDir(path); err == nil {
						f.ctx.LogDebug(fmt.Sprintf("Cleaning up temporary directory %s (%d entries)", entryName, len(dirEntries)))
					}
				} else {
					totalSize += info.Size()
					f.ctx.LogDebug(fmt.Sprintf("Cleaning up temporary file %s (%d bytes)", entryName, info.Size()))
				}
			}

			cleanStart := time.Now()
			if err := os.RemoveAll(path); err != nil {
				failed = append(failed, entryName)
				f.ctx.LogDebug(fmt.Sprintf("Failed to clean up %s: %v", entryName, err))
			} else {
				cleanDuration := time.Since(cleanStart)
				cleaned = append(cleaned, entryName)
				f.ctx.LogDebug(fmt.Sprintf("Cleaned up %s in %v", entryName, cleanDuration))
			}
		} else {
			skipped = append(skipped, entryName)
			f.ctx.LogDebug(fmt.Sprintf("Keeping %s (not temporary)", entryName))
		}
	}

	if len(failed) > 0 {
		f.ctx.LogDebug(fmt.Sprintf("Failed to clean up %d items: %v", len(failed), failed))
	}

	if len(skipped) > 0 {
		f.ctx.LogDebug(fmt.Sprintf("Kept %d non-temporary items: %v", len(skipped), skipped))
	}

	if totalSize > 0 {
		f.ctx.LogInfo(fmt.Sprintf("Cleaned up %d/%d temporary items from destination %s (freed %d bytes)",
			len(cleaned), len(entries), absSandboxDir, totalSize))
	} else {
		f.ctx.LogInfo(fmt.Sprintf("Cleaned up %d/%d temporary items from destination %s", len(cleaned), len(entries), absSandboxDir))
	}

	f.logFunctionSuccess("cleanup", start, cleaned)
	return cleaned, nil
}

// log logs a message at the specified level
func (f *functions) log(level, message string) (bool, error) {
	start := f.logFunctionEntry("log", level, message)

	normalizedLevel := strings.ToLower(level)
	f.ctx.LogDebug(fmt.Sprintf("CEL log function called with level=%s", normalizedLevel))

	switch normalizedLevel {
	case "info":
		f.ctx.LogInfo(message)
	case "debug":
		f.ctx.LogDebug(message)
	case "error":
		f.ctx.LogError(message)
	default:
		f.ctx.LogInfo(fmt.Sprintf("[%s] %s", level, message))
		f.ctx.LogDebug(fmt.Sprintf("Unknown log level '%s', treated as info", level))
	}

	f.logFunctionSuccess("log", start, true)
	return true, nil
}

// fail immediately fails the pipeline with a message
func (f *functions) fail(message string) (bool, error) {
	start := f.logFunctionEntry("fail", message)

	f.ctx.LogError(fmt.Sprintf("CEL fail function called: %s", message))
	f.ctx.FailPipeline(message)

	f.logFunctionSuccess("fail", start, false)
	return false, nil
}

// Helper functions

// applyTypeFilter filters files by type
func (f *functions) applyTypeFilter(matches []string, typeFilter string) []string {
	if typeFilter == "" {
		f.ctx.LogDebug("No type filter specified, returning all matches")
		return matches
	}

	f.ctx.LogDebug(fmt.Sprintf("Applying type filter '%s' to %d files", typeFilter, len(matches)))

	var filtered []string
	var skipped []string

	for i, match := range matches {
		itemName := filepath.Base(match)
		f.ctx.LogDebug(fmt.Sprintf("Checking file %d/%d: %s", i+1, len(matches), itemName))

		info, err := os.Stat(match)
		if err != nil {
			f.ctx.LogDebug(fmt.Sprintf("Skipping %s: cannot stat file (%v)", itemName, err))
			skipped = append(skipped, itemName)
			continue
		}

		var matchesFilter bool
		var filterReason string

		switch typeFilter {
		case "dir":
			matchesFilter = info.IsDir()
			filterReason = fmt.Sprintf("is directory: %v", matchesFilter)
		case "executable":
			matchesFilter = !info.IsDir() && info.Mode()&0111 != 0
			filterReason = fmt.Sprintf("is executable: %v (mode: %o)", matchesFilter, info.Mode())
		case "archive":
			matchesFilter = f.isArchiveFile(match)
			filterReason = fmt.Sprintf("is archive: %v", matchesFilter)
		default:
			matchesFilter = true
			filterReason = "unknown filter, accepting all"
			f.ctx.LogDebug(fmt.Sprintf("Unknown type filter '%s', accepting all files", typeFilter))
		}

		f.ctx.LogDebug(fmt.Sprintf("File %s: %s", itemName, filterReason))

		if matchesFilter {
			filtered = append(filtered, match)
		} else {
			skipped = append(skipped, itemName)
		}
	}

	f.ctx.LogDebug(fmt.Sprintf("Type filter '%s': %d files matched, %d skipped", typeFilter, len(filtered), len(skipped)))
	if len(skipped) > 0 && len(skipped) <= 10 { // Only show skipped list if not too long
		f.ctx.LogDebug(fmt.Sprintf("Skipped files: %v", skipped))
	}

	return filtered
}

// isArchiveFile checks if a file is an archive based on extension
func (f *functions) isArchiveFile(path string) bool {
	filename := filepath.Base(path)
	lower := strings.ToLower(filename)
	extensions := []string{".tar", ".tar.gz", ".tgz", ".tar.xz", ".txz", ".zip", ".jar"}

	for _, ext := range extensions {
		if strings.HasSuffix(lower, ext) {
			f.ctx.LogDebug(fmt.Sprintf("File %s identified as archive (extension: %s)", filename, ext))
			return true
		}
	}

	f.ctx.LogDebug(fmt.Sprintf("File %s is not an archive", filename))
	return false
}

// isTemporaryFile determines if a file should be cleaned up
func (f *functions) isTemporaryFile(name string) bool {
	// Common temporary file patterns
	if strings.HasPrefix(name, ".") {
		f.ctx.LogDebug(fmt.Sprintf("File %s is temporary (hidden file)", name))
		return true
	}

	if strings.HasPrefix(name, "~") {
		f.ctx.LogDebug(fmt.Sprintf("File %s is temporary (backup file)", name))
		return true
	}

	if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".temp") {
		f.ctx.LogDebug(fmt.Sprintf("File %s is temporary (temp extension)", name))
		return true
	}

	// Common build artifacts and documentation
	buildArtifacts := []string{"LICENSE", "README", "README.md", "CHANGELOG", "CHANGELOG.md", "NOTICE"}
	for _, artifact := range buildArtifacts {
		if name == artifact {
			f.ctx.LogDebug(fmt.Sprintf("File %s is temporary (build artifact)", name))
			return true
		}
	}

	f.ctx.LogDebug(fmt.Sprintf("File %s is not temporary", name))
	return false
}

// promoteDirectoryContents moves directory contents to root level
func (f *functions) promoteDirectoryContents(targetDir string) error {
	dirName := filepath.Base(targetDir)
	absTargetDir, _ := filepath.Abs(targetDir)
	absSandboxDir, _ := filepath.Abs(f.ctx.SandboxDir)
	absTmpDir, _ := filepath.Abs(f.ctx.TmpDir)

	f.ctx.LogDebug(fmt.Sprintf("Starting directory promotion for: %s from %s", dirName, absTargetDir))

	// Create a temporary directory to hold the contents
	f.ctx.LogDebug(fmt.Sprintf("Creating temporary directory for promotion staging in %s", absTmpDir))
	tempDir, err := os.MkdirTemp(f.ctx.TmpDir, "chdir-temp-")
	if err != nil {
		f.ctx.LogError(fmt.Sprintf("Failed to create temp directory: %v", err))
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		absTempDir, _ := filepath.Abs(tempDir)
		f.ctx.LogDebug(fmt.Sprintf("Cleaning up temporary directory: %s", absTempDir))
		os.RemoveAll(tempDir)
	}()

	absTempDir, _ := filepath.Abs(tempDir)
	f.ctx.LogDebug(fmt.Sprintf("Created temporary directory: %s", absTempDir))

	// Move target directory contents to temp
	f.ctx.LogDebug(fmt.Sprintf("Reading target directory contents: %s", absTargetDir))
	targetEntries, err := os.ReadDir(targetDir)
	if err != nil {
		f.ctx.LogError(fmt.Sprintf("Failed to read target directory %s: %v", absTargetDir, err))
		return fmt.Errorf("failed to read target directory: %w", err)
	}

	f.ctx.LogInfo(fmt.Sprintf("Moving %d items from %s to temporary staging at %s", len(targetEntries), absTargetDir, absTempDir))
	for i, entry := range targetEntries {
		src := filepath.Join(targetDir, entry.Name())
		dst := filepath.Join(tempDir, entry.Name())

		f.ctx.LogDebug(fmt.Sprintf("Moving item %d/%d to temp: %s", i+1, len(targetEntries), entry.Name()))

		if err := os.Rename(src, dst); err != nil {
			moveErr := fmt.Errorf("failed to move %s to temp: %w", entry.Name(), err)
			f.ctx.LogError(moveErr.Error())
			return moveErr
		}
	}

	// Remove everything in destination directory
	f.ctx.LogDebug(fmt.Sprintf("Clearing destination directory: %s", absSandboxDir))
	entries, err := os.ReadDir(f.ctx.SandboxDir)
	if err != nil {
		f.ctx.LogError(fmt.Sprintf("Failed to read destination directory %s: %v", absSandboxDir, err))
		return fmt.Errorf("failed to read destination directory: %w", err)
	}

	f.ctx.LogInfo(fmt.Sprintf("Removing %d items from destination %s", len(entries), absSandboxDir))
	var removedCount int
	for i, entry := range entries {
		path := filepath.Join(f.ctx.SandboxDir, entry.Name())
		absPath, _ := filepath.Abs(path)
		f.ctx.LogDebug(fmt.Sprintf("Removing destination item %d/%d: %s from %s", i+1, len(entries), entry.Name(), absPath))

		if err := os.RemoveAll(path); err != nil {
			f.ctx.LogDebug(fmt.Sprintf("Failed to remove %s: %v", entry.Name(), err))
		} else {
			removedCount++
		}
	}

	f.ctx.LogDebug(fmt.Sprintf("Successfully removed %d/%d items from destination %s", removedCount, len(entries), absSandboxDir))

	// Move everything from temp back to destination
	f.ctx.LogDebug(fmt.Sprintf("Moving items from temporary staging %s to destination root %s", absTempDir, absSandboxDir))
	tempEntries, err := os.ReadDir(tempDir)
	if err != nil {
		f.ctx.LogError(fmt.Sprintf("Failed to read temp directory %s: %v", absTempDir, err))
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

	f.ctx.LogInfo(fmt.Sprintf("Promoting %d items to destination root level %s", len(tempEntries), absSandboxDir))
	for i, entry := range tempEntries {
		src := filepath.Join(tempDir, entry.Name())
		dst := filepath.Join(f.ctx.SandboxDir, entry.Name())
		absSrc, _ := filepath.Abs(src)
		absDst, _ := filepath.Abs(dst)

		f.ctx.LogDebug(fmt.Sprintf("Promoting item %d/%d: %s from %s to %s", i+1, len(tempEntries), entry.Name(), absSrc, absDst))

		if err := os.Rename(src, dst); err != nil {
			moveErr := fmt.Errorf("failed to move %s from %s to destination %s: %w", entry.Name(), absSrc, absSandboxDir, err)
			f.ctx.LogError(moveErr.Error())
			return moveErr
		}
	}

	f.ctx.LogInfo(fmt.Sprintf("Successfully promoted %s contents from %s to destination root level %s", dirName, absTargetDir, absSandboxDir))
	return nil
}

// CEL wrapper functions - these adapt Go function signatures to CEL ref.Val types

// globCEL wraps glob function for CEL
func (f *functions) globCEL(arg ref.Val) ref.Val {
	pattern, ok := arg.Value().(string)
	if !ok {
		return types.NewErr("glob: expected string argument")
	}

	result, err := f.glob(pattern)
	if err != nil {
		return newCELError("glob", err)
	}

	// Convert []string to CEL list
	celItems := make([]ref.Val, len(result))
	for i, item := range result {
		celItems[i] = types.String(item)
	}
	return types.NewDynamicList(types.DefaultTypeAdapter, celItems)
}

// unarchiveCEL wraps unarchive function for CEL
func (f *functions) unarchiveCEL(arg ref.Val) ref.Val {
	archive, ok := arg.Value().(string)
	if !ok {
		return types.NewErr("unarchive: expected string argument")
	}

	result, err := f.unarchive(archive)
	if err != nil {
		return newCELError("unarchive", err)
	}

	return types.Int(result)
}

// moveCEL wraps move function for CEL
func (f *functions) moveCEL(lhs, rhs ref.Val) ref.Val {
	src, ok1 := lhs.Value().(string)
	dst, ok2 := rhs.Value().(string)
	if !ok1 || !ok2 {
		return types.NewErr("move: expected string arguments")
	}

	result, err := f.move(src, dst)
	if err != nil {
		return newCELError("move", err)
	}

	return types.String(result)
}

// chdirCEL wraps chdir function for CEL
func (f *functions) chdirCEL(arg ref.Val) ref.Val {
	pattern, ok := arg.Value().(string)
	if !ok {
		return types.NewErr("chdir: expected string argument")
	}

	result, err := f.chdir(pattern)
	if err != nil {
		return newCELError("chdir", err)
	}

	return types.String(result)
}

// chmodCEL wraps chmod function for CEL
func (f *functions) chmodCEL(lhs, rhs ref.Val) ref.Val {
	pattern, ok1 := lhs.Value().(string)
	mode, ok2 := rhs.Value().(string)
	if !ok1 || !ok2 {
		return types.NewErr("chmod: expected string arguments")
	}

	result, err := f.chmod(pattern, mode)
	if err != nil {
		return newCELError("chmod", err)
	}

	// Convert []string to CEL list
	celItems := make([]ref.Val, len(result))
	for i, item := range result {
		celItems[i] = types.String(item)
	}
	return types.NewDynamicList(types.DefaultTypeAdapter, celItems)
}

// deleteCEL wraps delete function for CEL
// Accepts either a string pattern or a list of paths:
//
//	delete("*.bat") - glob pattern
//	delete(glob("*.bat")) - list of paths from glob()
func (f *functions) deleteCEL(arg ref.Val) ref.Val {
	var result []string
	var err error

	// Check if argument is a list (from glob() results)
	if lister, ok := arg.(traits.Lister); ok {
		// Convert CEL list to []string
		size := int(lister.Size().Value().(int64))
		paths := make([]string, size)
		for i := 0; i < size; i++ {
			item := lister.Get(types.Int(i))
			path, ok := item.Value().(string)
			if !ok {
				return types.NewErr("delete: list must contain only strings")
			}
			paths[i] = path
		}
		result, err = f.deleteList(paths)
	} else if pattern, ok := arg.Value().(string); ok {
		// String pattern - use glob-based delete
		result, err = f.delete(pattern)
	} else {
		return types.NewErr("delete: expected string pattern or list of paths")
	}

	if err != nil {
		return newCELError("delete", err)
	}

	// Convert []string to CEL list
	celItems := make([]ref.Val, len(result))
	for i, item := range result {
		celItems[i] = types.String(item)
	}
	return types.NewDynamicList(types.DefaultTypeAdapter, celItems)
}

// cleanupCEL wraps cleanup function for CEL
func (f *functions) cleanupCEL(args ...ref.Val) ref.Val {
	if len(args) != 0 {
		return types.NewErr("cleanup: expected no arguments")
	}

	result, err := f.cleanup()
	if err != nil {
		return newCELError("cleanup", err)
	}

	// Convert []string to CEL list
	celItems := make([]ref.Val, len(result))
	for i, item := range result {
		celItems[i] = types.String(item)
	}
	return types.NewDynamicList(types.DefaultTypeAdapter, celItems)
}

// logCEL wraps log function for CEL
func (f *functions) logCEL(lhs, rhs ref.Val) ref.Val {
	level, ok1 := lhs.Value().(string)
	message, ok2 := rhs.Value().(string)
	if !ok1 || !ok2 {
		return types.NewErr("log: expected string arguments")
	}

	result, err := f.log(level, message)
	if err != nil {
		return newCELError("log", err)
	}

	return types.Bool(result)
}

// failCEL wraps fail function for CEL
func (f *functions) failCEL(arg ref.Val) ref.Val {
	message, ok := arg.Value().(string)
	if !ok {
		return types.NewErr("fail: expected string argument")
	}

	result, err := f.fail(message)
	if err != nil {
		return newCELError("fail", err)
	}

	return types.Bool(result)
}
