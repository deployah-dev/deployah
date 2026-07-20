package helm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"deployah.dev/deployah/internal/spec"
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

// GenerateCacheKey creates a cache key from the resolved spec (or raw spec
// when resolved is nil) and the embedded chart template hash.
//
// When resolved is non-nil it is hashed instead of the full raw spec: this
// covers only the target-environment subset and ensures platform file changes
// invalidate the cache. encoding/json sorts map keys deterministically since
// Go 1.12, so the serialization is stable.
func GenerateCacheKey(manifest *spec.Spec, resolved *spec.ResolvedSpec) (string, error) {
	var inputBytes []byte
	var err error
	if resolved != nil {
		inputBytes, err = json.Marshal(resolved)
	} else {
		inputBytes, err = json.Marshal(manifest)
	}
	if err != nil {
		return "", fmt.Errorf("failed to marshal spec for hashing: %w", err)
	}

	// Get hash of embedded chart templates to detect chart updates.
	chartHash, err := getEmbeddedChartHash()
	if err != nil {
		return "", fmt.Errorf("failed to generate embedded chart hash: %w", err)
	}

	specHash := sha256.Sum256(inputBytes)
	combinedData := fmt.Sprintf("%s-%s", hex.EncodeToString(specHash[:]), chartHash)
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
	if _, err := os.Stat(cache.Path); errors.Is(err, fs.ErrNotExist) {
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
				slog.Warn("failed to cleanup expired chart cache", "path", cache.Path, "err", err)
			}
			delete(chartCache, key)
		}
	}
}

// ClearChartCache clears all cached charts and removes their directories
func ClearChartCache() error {
	chartCacheMutex.Lock()
	defer chartCacheMutex.Unlock()

	var errs []error
	for key, cache := range chartCache {
		if err := os.RemoveAll(cache.Path); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove %s: %w", cache.Path, err))
		}
		delete(chartCache, key)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during cache cleanup: %w", errors.Join(errs...))
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
		sourceFile, err := os.Open(path) // #nosec G304,G122 -- path from filepath.Walk within source tree
		if err != nil {
			return fmt.Errorf("failed to open source file %s: %w", path, err)
		}

		destFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode()) // #nosec G304 -- dest under controlled tmpDir
		if err != nil {
			if closeErr := sourceFile.Close(); closeErr != nil {
				return fmt.Errorf("failed to create destination file %s: %w", destPath, errors.Join(err, closeErr))
			}
			return fmt.Errorf("failed to create destination file %s: %w", destPath, err)
		}

		_, copyErr := destFile.ReadFrom(sourceFile)
		srcCloseErr := sourceFile.Close()
		dstCloseErr := destFile.Close()
		if copyErr != nil {
			return fmt.Errorf("failed to copy file from %s to %s: %w", path, destPath, copyErr)
		}
		if srcCloseErr != nil {
			return fmt.Errorf("failed to close source file %s: %w", path, srcCloseErr)
		}
		if dstCloseErr != nil {
			return fmt.Errorf("failed to close destination file %s: %w", destPath, dstCloseErr)
		}
		return nil
	})
	if err != nil {
		if removeErr := os.RemoveAll(tmpDir); removeErr != nil {
			return "", fmt.Errorf("failed to copy cached chart: %w", errors.Join(err, removeErr))
		}
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
