package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	runtimeCache      = make(map[string]*runtimeInfo)
	runtimeCacheMutex sync.RWMutex
	cacheFilePath     string
)

func init() {
	// Initialize cache file path in user's home directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		cacheFilePath = filepath.Join(homeDir, ".deps", "cache", "runtimes.json")
	}

	// Load cache from disk on startup
	loadCacheFromDisk()
}

// getCachedRuntime retrieves cached runtime info for a language
func getCachedRuntime(language string) (*runtimeInfo, bool) {
	runtimeCacheMutex.RLock()
	defer runtimeCacheMutex.RUnlock()

	info, exists := runtimeCache[language]
	return info, exists
}

// setCachedRuntime stores runtime info in cache and persists to disk
func setCachedRuntime(language string, info *runtimeInfo) error {
	runtimeCacheMutex.Lock()
	defer runtimeCacheMutex.Unlock()

	runtimeCache[language] = info
	return saveCacheToDisk()
}

// invalidateCache removes a specific runtime from cache
func invalidateCache(language string) error {
	runtimeCacheMutex.Lock()
	defer runtimeCacheMutex.Unlock()

	delete(runtimeCache, language)
	return saveCacheToDisk()
}

// loadCacheFromDisk reads the cache file into memory
func loadCacheFromDisk() error {
	if cacheFilePath == "" {
		return nil // No cache path available
	}

	data, err := os.ReadFile(cacheFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Cache file doesn't exist yet, that's okay
		}
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	runtimeCacheMutex.Lock()
	defer runtimeCacheMutex.Unlock()

	if err := json.Unmarshal(data, &runtimeCache); err != nil {
		return fmt.Errorf("failed to parse cache file: %w", err)
	}

	return nil
}

// saveCacheToDisk writes the cache to disk (caller must hold lock)
func saveCacheToDisk() error {
	if cacheFilePath == "" {
		return nil // No cache path available
	}

	// Ensure cache directory exists
	cacheDir := filepath.Dir(cacheFilePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	data, err := json.MarshalIndent(runtimeCache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(cacheFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}
