// Package embedder cache stores embeddings locally by chunk hash.
// If an embedding was already generated for a chunk, it is reused
// without calling the provider API — saving time and money.
//
// Cache location: ~/.brain/cache/<project_id>/embeddings.json
package embedder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// EmbedCache stores chunk_hash → embedding vector on disk.
type EmbedCache struct {
	path    string
	entries map[string][]float32
	mu      sync.RWMutex
	dirty   bool
}

// NewCache loads or creates an embedding cache for a project.
func NewCache(projectID string) (*EmbedCache, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	dir := filepath.Join(home, ".brain", "cache", projectID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	path := filepath.Join(dir, "embeddings.json")
	c := &EmbedCache{
		path:    path,
		entries: make(map[string][]float32),
	}

	// Load existing cache
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &c.entries)
	}

	return c, nil
}

// Get returns the cached embedding for a chunk hash, or nil if not cached.
func (c *EmbedCache) Get(chunkHash string) []float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[chunkHash]
}

// Set stores an embedding for a chunk hash.
func (c *EmbedCache) Set(chunkHash string, embedding []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[chunkHash] = embedding
	c.dirty = true
}

// Len returns the number of cached embeddings.
func (c *EmbedCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Save persists the cache to disk. Call after indexing completes.
func (c *EmbedCache) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.dirty {
		return nil
	}

	data, err := json.Marshal(c.entries)
	if err != nil {
		return fmt.Errorf("marshal embed cache: %w", err)
	}
	if err := os.WriteFile(c.path, data, 0644); err != nil {
		return fmt.Errorf("write embed cache: %w", err)
	}
	return nil
}
