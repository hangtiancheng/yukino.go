package yukino_cache

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type Cache struct {
	mu          sync.RWMutex
	store       Store
	opts        CacheOptions
	hits        atomic.Int64
	misses      atomic.Int64
	initialized atomic.Int32
	closed      atomic.Int32
}

type CacheOptions struct {
	MaxBytes      int64
	BucketCount   uint16
	CapPerBucket  uint16
	Level2Cap     uint16
	CleanupTime   time.Duration
	OnEvicted     func(key string, value Value)
	DashboardAddr string
}

func DefaultCacheOptions() CacheOptions {
	return CacheOptions{
		MaxBytes:     8 * 1024 * 1024,
		BucketCount:  16,
		CapPerBucket: 512,
		Level2Cap:    256,
		CleanupTime:  time.Minute,
		OnEvicted:    nil,
	}
}

func NewCache(opts CacheOptions) *Cache {
	return &Cache{opts: opts}
}

func (c *Cache) ensureInitialized() {
	if c.initialized.Load() == 1 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed.Load() == 1 {
		return
	}
	if c.initialized.Load() == 0 {
		storeOpts := StoreOptions{
			MaxBytes:        c.opts.MaxBytes,
			BucketCount:     c.opts.BucketCount,
			CapPerBucket:    c.opts.CapPerBucket,
			Level2Cap:       c.opts.Level2Cap,
			CleanupInterval: c.opts.CleanupTime,
			OnEvicted:       c.opts.OnEvicted,
		}
		c.store = NewStore(storeOpts)
		c.initialized.Store(1)
		log.Printf("Cache initialized, max bytes: %d", c.opts.MaxBytes)
	}
}

func (c *Cache) Add(key string, value ByteView) {
	if c.closed.Load() == 1 {
		log.Printf("Attempted to add to a closed cache: %s", key)
		return
	}

	c.ensureInitialized()

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.store == nil {
		return
	}
	if err := c.store.Set(key, value); err != nil {
		log.Printf("Failed to add key %s to cache: %v", key, err)
	}
}

func (c *Cache) Get(ctx context.Context, key string) (ByteView, bool) {
	if c.closed.Load() == 1 {
		return ByteView{}, false
	}
	if c.initialized.Load() == 0 {
		c.misses.Add(1)
		return ByteView{}, false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.store == nil {
		c.misses.Add(1)
		return ByteView{}, false
	}

	val, found := c.store.Get(key)
	if !found {
		c.misses.Add(1)
		return ByteView{}, false
	}

	bv, ok := val.(ByteView)
	if !ok {
		log.Printf("Type assertion failed for key %s, expected ByteView", key)
		c.misses.Add(1)
		return ByteView{}, false
	}

	c.hits.Add(1)
	return bv, true
}

func (c *Cache) AddWithExpiration(key string, value ByteView, expirationTime time.Time) {
	if c.closed.Load() == 1 {
		log.Printf("Attempted to add to a closed cache: %s", key)
		return
	}

	expiration := time.Until(expirationTime)
	if expiration <= 0 {
		log.Printf("Key %s already expired, not adding to cache", key)
		return
	}

	c.ensureInitialized()

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.store == nil {
		return
	}
	if err := c.store.SetWithExpiration(key, value, expiration); err != nil {
		log.Printf("Failed to add key %s to cache with expiration: %v", key, err)
	}
}

func (c *Cache) Delete(key string) bool {
	if c.closed.Load() == 1 || c.initialized.Load() == 0 {
		return false
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.store == nil {
		return false
	}
	return c.store.Delete(key)
}

func (c *Cache) Clear() {
	if c.closed.Load() == 1 || c.initialized.Load() == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.store == nil {
		return
	}
	c.store.Clear()
	c.hits.Store(0)
	c.misses.Store(0)
}

func (c *Cache) Len() int {
	if c.closed.Load() == 1 || c.initialized.Load() == 0 {
		return 0
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.store == nil {
		return 0
	}
	return c.store.Len()
}

func (c *Cache) Close() {
	if !c.closed.CompareAndSwap(0, 1) {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.store != nil {
		c.store.Close()
		c.store = nil
	}
	c.initialized.Store(0)
	log.Printf("Cache closed, hits: %d, misses: %d", c.hits.Load(), c.misses.Load())
}

func (c *Cache) DashboardEnabled() bool {
	return c.opts.DashboardAddr != ""
}

func (c *Cache) Entries() []Entry {
	if c.closed.Load() == 1 || c.initialized.Load() == 0 {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.store == nil {
		return nil
	}

	var entries []Entry
	c.store.Walk(func(e Entry) bool {
		entries = append(entries, e)
		return true
	})
	return entries
}

func (c *Cache) Stats() map[string]any {
	stats := map[string]any{
		"initialized": c.initialized.Load() == 1,
		"closed":      c.closed.Load() == 1,
		"hits":        c.hits.Load(),
		"misses":      c.misses.Load(),
	}

	if c.initialized.Load() == 1 {
		stats["size"] = c.Len()
		totalRequests := stats["hits"].(int64) + stats["misses"].(int64)
		if totalRequests > 0 {
			stats["hit_rate"] = float64(stats["hits"].(int64)) / float64(totalRequests)
		} else {
			stats["hit_rate"] = 0.0
		}

		c.mu.RLock()
		if s, ok := c.store.(*lruStore); ok {
			stats["bytes"] = s.Bytes()
			stats["evictions"] = s.Evictions()
		}
		c.mu.RUnlock()
	}

	return stats
}
