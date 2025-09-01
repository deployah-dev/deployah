package helm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/deployah-dev/deployah/internal/manifest"
)

// ChartCache represents a cached chart entry
type ChartCache struct {
	Path      string
	CreatedAt time.Time
}

// ChartCacheInfo provides detailed information about the chart cache
type ChartCacheInfo struct {
	Count     int      `json:"count"`
	TotalSize int64    `json:"totalSize"`
	ChartHash string   `json:"chartHash"`
	TTL       string   `json:"ttl"`
	CacheKeys []string `json:"cacheKeys,omitempty"`
}

// chartCache is a global cache for prepared charts
var (
	chartCache        = make(map[string]*ChartCache)
	chartCacheMutex   sync.RWMutex
	chartCacheTTL     = 1 * time.Hour // Cache TTL - charts expire after 1 hour
	embeddedChartHash string          // Cached hash of embedded chart templates
	chartHashOnce     sync.Once       // Ensure embedded chart hash is computed only once
)

// GenerateCacheKey creates a comprehensive cache key that includes both manifest and chart template hashes
func GenerateCacheKey(manifest *manifest.Manifest) (string, error) {
	// Create a deterministic representation of the manifest for hashing
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal manifest for hashing: %w", err)
	}

	// Get hash of embedded chart templates to detect chart updates
	chartHash, err := getEmbeddedChartHash()
	if err != nil {
		return "", fmt.Errorf("failed to generate embedded chart hash: %w", err)
	}

	// Combine manifest hash and chart hash for comprehensive cache key
	manifestHash := sha256.Sum256(manifestBytes)
	combinedData := fmt.Sprintf("%s-%s", hex.EncodeToString(manifestHash[:]), chartHash)
	finalHash := sha256.Sum256([]byte(combinedData))

	return hex.EncodeToString(finalHash[:]), nil
}

// getEmbeddedChartHash generates a hash of the embedded chart templates
// This ensures cache invalidation when the base Deployah chart is updated
// The hash is computed only once per application run for performance
func getEmbeddedChartHash() (string, error) {
	var err error

	// Use sync.Once to ensure the hash is computed only once per application run
	chartHashOnce.Do(func() {
		hasher := sha256.New()

		// Walk through all embedded chart files and hash their content
		walkErr := fs.WalkDir(ChartTemplateFS, "chart", func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("failed to access embedded chart file %s: %w", path, walkErr)
			}

			// Skip directories, only hash file contents
			if d.IsDir() {
				return nil
			}

			// Read file content
			data, readErr := ChartTemplateFS.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", path, readErr)
			}

			// Include file path and content in hash to detect both content and structure changes
			hasher.Write([]byte(path))
			hasher.Write(data)

			return nil
		})

		if walkErr != nil {
			err = fmt.Errorf("failed to walk embedded chart directory: %w", walkErr)
			return
		}

		embeddedChartHash = hex.EncodeToString(hasher.Sum(nil))
	})

	if err != nil {
		return "", fmt.Errorf("failed to generate embedded chart hash: %w", err)
	}

	return embeddedChartHash, nil
}

// GetCachedChart retrieves a cached chart if it exists and is valid
func GetCachedChart(cacheKey string) (string, bool) {
	chartCacheMutex.RLock()
	defer chartCacheMutex.RUnlock()

	cache, exists := chartCache[cacheKey]
	if !exists {
		return "", false
	}

	// Check if cache entry has expired
	if time.Since(cache.CreatedAt) > chartCacheTTL {
		return "", false
	}

	// Verify the cached directory still exists
	if _, err := os.Stat(cache.Path); os.IsNotExist(err) {
		return "", false
	}

	return cache.Path, true
}

// SetCachedChart stores a chart path in the cache
func SetCachedChart(cacheKey, chartPath string) {
	chartCacheMutex.Lock()
	defer chartCacheMutex.Unlock()

	chartCache[cacheKey] = &ChartCache{
		Path:      chartPath,
		CreatedAt: time.Now(),
	}
}

// CleanupExpiredCharts removes expired chart cache entries
func CleanupExpiredCharts() {
	chartCacheMutex.Lock()
	defer chartCacheMutex.Unlock()

	now := time.Now()
	for key, cache := range chartCache {
		if now.Sub(cache.CreatedAt) > chartCacheTTL {
			// Remove expired chart directory
			if err := os.RemoveAll(cache.Path); err != nil {
				// Log error but continue cleanup
				fmt.Printf("Warning: failed to cleanup expired chart cache at %s: %v\n", cache.Path, err)
			}
			delete(chartCache, key)
		}
	}
}

// ClearChartCache clears all cached charts and removes their directories
func ClearChartCache() error {
	chartCacheMutex.Lock()
	defer chartCacheMutex.Unlock()

	var errors []string
	for key, cache := range chartCache {
		if err := os.RemoveAll(cache.Path); err != nil {
			errors = append(errors, fmt.Sprintf("failed to remove %s: %v", cache.Path, err))
		}
		delete(chartCache, key)
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors during cache cleanup: %s", strings.Join(errors, "; "))
	}

	return nil
}

// GetChartCacheStats returns statistics about the chart cache
func GetChartCacheStats() (count int, totalSize int64) {
	chartCacheMutex.RLock()
	defer chartCacheMutex.RUnlock()

	count = len(chartCache)
	for _, cache := range chartCache {
		if size, err := getDirSize(cache.Path); err == nil {
			totalSize += size
		}
	}

	return count, totalSize
}

// GetChartCacheInfo returns detailed information about the chart cache
func GetChartCacheInfo() (*ChartCacheInfo, error) {
	chartCacheMutex.RLock()
	defer chartCacheMutex.RUnlock()

	info := &ChartCacheInfo{
		Count: len(chartCache),
		TTL:   chartCacheTTL.String(),
	}

	// Get embedded chart hash
	if hash, err := getEmbeddedChartHash(); err == nil {
		info.ChartHash = hash
	}

	// Calculate total size and collect cache keys
	for key, cache := range chartCache {
		info.CacheKeys = append(info.CacheKeys, key)
		if size, err := getDirSize(cache.Path); err == nil {
			info.TotalSize += size
		}
	}

	return info, nil
}

// CreateChartCopy creates a copy of a cached chart directory to avoid conflicts
func CreateChartCopy(sourcePath string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "deployah-chart-copy-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir for chart copy: %w", err)
	}

	// Copy the entire directory tree
	err = filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to access path %s: %w", path, err)
		}

		// Calculate relative path
		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return fmt.Errorf("failed to calculate relative path for %s: %w", path, err)
		}

		destPath := filepath.Join(tmpDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		// Copy file
		sourceFile, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open source file %s: %w", path, err)
		}
		defer sourceFile.Close()

		destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return fmt.Errorf("failed to create destination file %s: %w", destPath, err)
		}
		defer destFile.Close()

		_, err = destFile.ReadFrom(sourceFile)
		if err != nil {
			return fmt.Errorf("failed to copy file from %s to %s: %w", path, destPath, err)
		}
		return nil
	})

	if err != nil {
		os.RemoveAll(tmpDir) // Cleanup on error
		return "", fmt.Errorf("failed to copy cached chart: %w", err)
	}

	return tmpDir, nil
}

// getDirSize calculates the total size of a directory
func getDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to access file in directory %s: %w", path, err)
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}
