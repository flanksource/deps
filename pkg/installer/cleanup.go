package installer

import (
	"os"

	"github.com/flanksource/clicky/task"
	"github.com/flanksource/deps/pkg/utils"
)

// CleanupManager handles temporary file cleanup with debug and preserve logic
type CleanupManager struct {
	debug             bool
	shouldSkipCleanup bool
	files             []string
	directories       []string
	task              *task.Task
}

// NewCleanupManager creates a new cleanup manager
func NewCleanupManager(debug, shouldSkipCleanup bool, t *task.Task) *CleanupManager {
	return &CleanupManager{
		debug:             debug,
		shouldSkipCleanup: shouldSkipCleanup,
		task:              t,
		files:             make([]string, 0),
		directories:       make([]string, 0),
	}
}

// AddFile adds a file to be cleaned up
func (cm *CleanupManager) AddFile(filepath string) {
	if filepath != "" {
		cm.files = append(cm.files, filepath)
	}
}

// AddDirectory adds a directory to be cleaned up
func (cm *CleanupManager) AddDirectory(dirpath string) {
	if dirpath != "" {
		cm.directories = append(cm.directories, dirpath)
	}
}

// GetCleanupFunc returns a cleanup function that can be deferred
func (cm *CleanupManager) GetCleanupFunc() func() {
	return func() {
		cm.Cleanup()
	}
}

// Cleanup performs the actual cleanup
func (cm *CleanupManager) Cleanup() {
	if cm.debug || cm.shouldSkipCleanup {
		if cm.shouldSkipCleanup && cm.task != nil {
			if len(cm.files) > 0 || len(cm.directories) > 0 {
				allPaths := append(cm.files, cm.directories...)
				for _, path := range allPaths {
					cm.task.V(3).Infof("Keeping temporary files: %s", utils.LogPath(path))
				}
			}
		} else if cm.debug && cm.task != nil {
			if len(cm.files) > 0 || len(cm.directories) > 0 {
				allPaths := append(cm.files, cm.directories...)
				for _, path := range allPaths {
					cm.task.Debugf("Install: keeping temporary files for debugging: %s", path)
				}
			}
		}
		return
	}

	// Clean up directories first (they may contain files)
	for _, dir := range cm.directories {
		if err := os.RemoveAll(dir); err != nil && cm.task != nil {
			cm.task.V(4).Infof("Failed to clean up directory %s: %v", utils.LogPath(dir), err)
		}
	}

	// Clean up files
	for _, file := range cm.files {
		if err := os.Remove(file); err != nil && cm.task != nil {
			cm.task.V(4).Infof("Failed to clean up file %s: %v", utils.LogPath(file), err)
		}
	}
}
