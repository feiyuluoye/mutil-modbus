package utils

import (
	"sync"
	"time"
)

// ValueCache is a simple in-memory TTL cache for float64 values keyed by string.
// It is thread-safe and designed for small hot-path usage (e.g., point value dedup).
type ValueCache struct {
	mu   sync.Mutex
	ttl  time.Duration
	data map[string]entry
}

type entry struct {
	v  float64
	at time.Time
}

// NewValueCache creates a new cache with the given TTL. If ttl <= 0, it defaults to 1h.
func NewValueCache(ttl time.Duration) *ValueCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &ValueCache{ttl: ttl, data: make(map[string]entry, 1024)}
}

// GetValue returns the cached value if it exists and hasn't expired.
func (c *ValueCache) GetValue(key string) (float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data[key]
	if !ok {
		return 0, false
	}
	if time.Since(e.at) > c.ttl {
		delete(c.data, key)
		return 0, false
	}
	return e.v, true
}

// SetValue stores the value with the current timestamp.
func (c *ValueCache) SetValue(key string, v float64) {
	c.mu.Lock()
	c.data[key] = entry{v: v, at: time.Now()}
	c.mu.Unlock()
}

// SetTTL updates the cache TTL for subsequent get checks.
func (c *ValueCache) SetTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = time.Hour
	}
	c.mu.Lock()
	c.ttl = ttl
	c.mu.Unlock()
}
