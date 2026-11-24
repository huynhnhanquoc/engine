package http

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// The cache of a site's answers (.cache(…), http.md mục 2.2): a GET's answer that is ok, kept in memory for
// the duration written, by its whole URL and its headers (two tokens are two entries). It holds 1024 answers and
// 64 MiB at most, letting the oldest go first; an answer past its time is let go when it is read.
const (
	cacheEntries       = 1024
	cacheBytes   int64 = 64 << 20
)

// Cache is one site's answers kept.
type Cache struct {
	mu      sync.Mutex
	entries map[string]*keptAnswer
	order   []string // keys from the oldest saved
	bytes   int64
}

type keptAnswer struct {
	answer  answer
	expires time.Time
	size    int64
}

// NewCache is an empty cache for a site.
func NewCache() *Cache { return &Cache{entries: map[string]*keptAnswer{}} }

// load is the answer kept under key, a copy, while its time lasts.
func (c *Cache) load(key string) (*answer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(kept.expires) {
		c.forget(key)
		return nil, false
	}
	copied := kept.answer
	return &copied, true
}

// save keeps answer under key for ttl, letting the oldest go past the caps.
func (c *Cache) save(key string, got *answer, ttl time.Duration) {
	size := int64(len(got.body)) + 512
	if size > cacheBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forget(key)
	for len(c.order) > 0 && (len(c.entries) >= cacheEntries || c.bytes+size > cacheBytes) {
		oldest := c.order[0]
		c.order = c.order[1:]
		if kept, ok := c.entries[oldest]; ok {
			c.bytes -= kept.size
			delete(c.entries, oldest)
		}
	}
	c.entries[key] = &keptAnswer{answer: answer{status: got.status, header: got.header.Clone(), body: got.body}, expires: time.Now().Add(ttl), size: size}
	c.order = append(c.order, key)
	c.bytes += size
}

// forget lets key go; c.mu is held. Its place in order goes when the order reaches it.
func (c *Cache) forget(key string) {
	if kept, ok := c.entries[key]; ok {
		c.bytes -= kept.size
		delete(c.entries, key)
		for i, each := range c.order {
			if each == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				break
			}
		}
	}
}

// cacheKey is the key of a request: its whole URL and its headers, sorted.
func cacheKey(url string, header http.Header) string {
	hash := sha256.New()
	hash.Write([]byte(url))
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		hash.Write([]byte("\n" + name + ":" + strings.Join(header[name], ",")))
	}
	return hex.EncodeToString(hash.Sum(nil)[:16])
}
