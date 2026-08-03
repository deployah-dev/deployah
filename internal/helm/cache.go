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

const defaultChartCacheTTL = 1 * time.Hour

// chartCacheEntry is one prepared chart directory in a [ChartCache].
type chartCacheEntry struct {
	path      string
	createdAt time.Time
}

// ChartCache stores prepared chart directories keyed by content hash.
// Each [Client] owns one instance (see [NewClient] and [WithChartCache]).
// There is no process-global cache. Methods are safe for concurrent use.
type ChartCache struct {
	mu      sync.RWMutex
	entries map[string]*chartCacheEntry
	ttl     time.Duration

	hashOnce      sync.Once
	embeddedHash  string
	embeddedError error
}

// NewChartCache returns an empty chart cache with the given TTL.
// A non-positive ttl falls back to one hour.
func NewChartCache(ttl time.Duration) *ChartCache {
	if ttl <= 0 {
		ttl = defaultChartCacheTTL
	}
	return &ChartCache{
		entries: make(map[string]*chartCacheEntry),
		ttl:     ttl,
	}
}

// GenerateKey creates a cache key from the resolved spec (or raw spec when
// resolved is nil), the target environment, and this cache's embedded chart
// template hash.
//
// environment must be part of the key: [PrepareChart] bakes the
// environment-filtered component set and environment label into the cached
// chart's values.yaml, so rendering environment A then B for the same
// manifest must not reuse A's cached chart for B.
//
// When resolved is non-nil it is hashed instead of the full raw spec: this
// covers only the target-environment subset and ensures platform file changes
// invalidate the cache. encoding/json sorts map keys deterministically since
// Go 1.12, so the serialization is stable.
func (c *ChartCache) GenerateKey(manifest *spec.Spec, environment string, resolved *spec.ResolvedSpec) (string, error) {
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

	chartHash, err := c.embeddedChartHash()
	if err != nil {
		return "", fmt.Errorf("failed to generate embedded chart hash: %w", err)
	}

	specHash := sha256.Sum256(inputBytes)
	combinedData := fmt.Sprintf("%s-%s-%s", hex.EncodeToString(specHash[:]), environment, chartHash)
	finalHash := sha256.Sum256([]byte(combinedData))

	return hex.EncodeToString(finalHash[:]), nil
}

// embeddedChartHash returns a hash of the embedded chart templates, computed
// once per [ChartCache] instance so cache keys invalidate when the base
// Deployah chart changes.
func (c *ChartCache) embeddedChartHash() (string, error) {
	c.hashOnce.Do(func() {
		hasher := sha256.New()

		walkErr := fs.WalkDir(ChartTemplateFS, "chart", func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("failed to access embedded chart file %s: %w", path, walkErr)
			}
			if d.IsDir() {
				return nil
			}

			data, readErr := ChartTemplateFS.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", path, readErr)
			}

			hasher.Write([]byte(path))
			hasher.Write(data)
			return nil
		})

		if walkErr != nil {
			c.embeddedError = fmt.Errorf("failed to walk embedded chart directory: %w", walkErr)
			return
		}
		c.embeddedHash = hex.EncodeToString(hasher.Sum(nil))
	})

	if c.embeddedError != nil {
		return "", c.embeddedError
	}
	return c.embeddedHash, nil
}

// get returns a cached chart path when the entry exists, is unexpired, and
// the directory is still on disk.
func (c *ChartCache) get(cacheKey string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[cacheKey]
	if !exists {
		return "", false
	}
	if time.Since(entry.createdAt) > c.ttl {
		return "", false
	}
	if _, err := os.Stat(entry.path); errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	return entry.path, true
}

// set stores a chart path in the cache.
func (c *ChartCache) set(cacheKey, chartPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[cacheKey] = &chartCacheEntry{
		path:      chartPath,
		createdAt: time.Now(),
	}
}

// entryCount returns the number of cache entries (including expired ones
// not yet cleaned up).
func (c *ChartCache) entryCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// cleanupExpired removes expired chart cache entries and their directories.
func (c *ChartCache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.Sub(entry.createdAt) > c.ttl {
			if err := os.RemoveAll(entry.path); err != nil {
				slog.Warn("failed to cleanup expired chart cache", "path", entry.path, "err", err)
			}
			delete(c.entries, key)
		}
	}
}

// createChartCopy creates a copy of a cached chart directory so callers can
// delete their returned path without removing the cache's backing directory.
func createChartCopy(sourcePath string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "deployah-chart-copy-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir for chart copy: %w", err)
	}

	err = filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to access path %s: %w", path, err)
		}

		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return fmt.Errorf("failed to calculate relative path for %s: %w", path, err)
		}

		destPath := filepath.Join(tmpDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

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
