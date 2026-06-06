package dlp

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// Fingerprint is the content-addressed cache key for a segment. Plain SHA-256
// of the normalized text is sufficient: the cache is a performance optimization
// (each segment is classified once in its lifetime), not a security mechanism,
// so it needs no keyed HMAC.
func Fingerprint(normalizedText string) string {
	sum := sha256.Sum256([]byte(normalizedText))
	return hex.EncodeToString(sum[:])
}

// Cache is a bounded, concurrency-safe LRU mapping a segment fingerprint to its
// classification Result. A miss simply costs one classification, so eviction is
// never a correctness risk.
type Cache struct {
	mu      sync.Mutex
	max     int
	ll      *list.List
	entries map[string]*list.Element
}

type cacheEntry struct {
	key    string
	result Result
}

// NewCache returns an LRU holding up to max entries (max <= 0 disables caching).
func NewCache(max int) *Cache {
	return &Cache{max: max, ll: list.New(), entries: make(map[string]*list.Element)}
}

// Get returns the cached result for a fingerprint, if present.
func (c *Cache) Get(key string) (Result, bool) {
	if c == nil || c.max <= 0 {
		return Result{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*cacheEntry).result, true
	}
	return Result{}, false
}

// Put stores a result, evicting the least-recently-used entry if over capacity.
func (c *Cache) Put(key string, r Result) {
	if c == nil || c.max <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		el.Value.(*cacheEntry).result = r
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&cacheEntry{key: key, result: r})
	c.entries[key] = el
	if c.ll.Len() > c.max {
		oldest := c.ll.Back()
		if oldest != nil {
			c.ll.Remove(oldest)
			delete(c.entries, oldest.Value.(*cacheEntry).key)
		}
	}
}

// Len reports the number of cached entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
