package search

import (
	"strings"
	"sync"
	"time"
)

const (
	defaultCacheTTL     = 15 * time.Minute
	defaultMaxEntries   = 100
)

// cacheEntry holds a cached value with expiration
type cacheEntry struct {
	value     SearchResult
	expiresAt time.Time
	insertedAt time.Time
}

// searchCache implements a simple in-memory cache with TTL
type searchCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
	maxSize int
}

// newSearchCache creates a new search cache with default settings
func newSearchCache() *searchCache {
	return &searchCache{
		entries: make(map[string]*cacheEntry),
		ttl:     defaultCacheTTL,
		maxSize: defaultMaxEntries,
	}
}

// normalizeCacheKey converts a key to lowercase and trims whitespace
func normalizeCacheKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

// get retrieves a cached result if it exists and hasn't expired
func (c *searchCache) get(key string) (SearchResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normalizedKey := normalizeCacheKey(key)
	entry, exists := c.entries[normalizedKey]

	if !exists {
		return SearchResult{}, false
	}

	// Check if expired
	if time.Now().After(entry.expiresAt) {
		return SearchResult{}, false
	}

	return entry.value, true
}

// set stores a result in the cache with TTL
func (c *searchCache) set(key string, value SearchResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedKey := normalizeCacheKey(key)

	// Evict oldest entry if cache is full
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	now := time.Now()
	c.entries[normalizedKey] = &cacheEntry{
		value:      value,
		expiresAt:  now.Add(c.ttl),
		insertedAt: now,
	}
}

// evictOldest removes the oldest entry from the cache (LRU-style)
func (c *searchCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	first := true

	for key, entry := range c.entries {
		if first || entry.insertedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.insertedAt
			first = false
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// cleanup removes all expired entries
func (c *searchCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}
