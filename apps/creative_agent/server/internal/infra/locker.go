package infra

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Locker abstracts the NX-lock primitive (heartbeat lock / idempotency lock).
// Production uses RedisLocker (cross-process); without Redis it degrades to MemoryLocker (valid within one process).
type Locker interface {
	// SetNX writes and sets the TTL only when the key does not exist; returns whether the lock was acquired.
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
	// Expire refreshes the key's expiry (heartbeat renewal).
	Expire(ctx context.Context, key string, ttl time.Duration) error
	// Del deletes the key (releases the lock).
	Del(ctx context.Context, key string) error
}

// RedisLocker is the go-redis-based Locker implementation.
type RedisLocker struct {
	rdb *redis.Client
}

// NewRedisLocker creates the Redis-backed Locker.
func NewRedisLocker(rdb *redis.Client) *RedisLocker { return &RedisLocker{rdb: rdb} }

// SetNX implements Locker.
func (l *RedisLocker) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return l.rdb.SetNX(ctx, key, value, ttl).Result()
}

// Expire implements Locker.
func (l *RedisLocker) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return l.rdb.Expire(ctx, key, ttl).Err()
}

// Del implements Locker.
func (l *RedisLocker) Del(ctx context.Context, key string) error {
	return l.rdb.Del(ctx, key).Err()
}

type memLockEntry struct {
	expireAt time.Time
}

// MemoryLocker is the in-process Locker implementation (degradation without Redis, test-friendly).
type MemoryLocker struct {
	mu      sync.Mutex
	entries map[string]memLockEntry
}

// NewMemoryLocker creates the in-process Locker.
func NewMemoryLocker() *MemoryLocker {
	return &MemoryLocker{entries: make(map[string]memLockEntry)}
}

// SetNX implements Locker.
func (l *MemoryLocker) SetNX(_ context.Context, key string, _ any, ttl time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if entry, ok := l.entries[key]; ok && entry.expireAt.After(now) {
		return false, nil
	}
	l.entries[key] = memLockEntry{expireAt: now.Add(ttl)}
	return true, nil
}

// Expire implements Locker. Returns nil when the key does not exist (an acceptable difference from Redis: heartbeats are only called while holding the lock).
func (l *MemoryLocker) Expire(_ context.Context, key string, ttl time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.entries[key]; ok {
		l.entries[key] = memLockEntry{expireAt: time.Now().Add(ttl)}
	}
	return nil
}

// Del implements Locker.
func (l *MemoryLocker) Del(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
	return nil
}
