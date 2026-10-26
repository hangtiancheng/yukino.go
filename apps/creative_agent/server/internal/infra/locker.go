package infra

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Locker 抽象 NX 锁原语（心跳锁 / 幂等锁）。
// 生产用 RedisLocker（跨进程），无 Redis 时降级 MemoryLocker（单进程内有效）。
type Locker interface {
	// SetNX 仅当 key 不存在时写入并设置 TTL，返回是否抢到锁。
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)
	// Expire 刷新 key 的过期时间（心跳续期）。
	Expire(ctx context.Context, key string, ttl time.Duration) error
	// Del 删除 key（释放锁）。
	Del(ctx context.Context, key string) error
}

// RedisLocker 基于 go-redis 的 Locker 实现。
type RedisLocker struct {
	rdb *redis.Client
}

// NewRedisLocker 创建 Redis 版 Locker。
func NewRedisLocker(rdb *redis.Client) *RedisLocker { return &RedisLocker{rdb: rdb} }

// SetNX 实现 Locker。
func (l *RedisLocker) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return l.rdb.SetNX(ctx, key, value, ttl).Result()
}

// Expire 实现 Locker。
func (l *RedisLocker) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return l.rdb.Expire(ctx, key, ttl).Err()
}

// Del 实现 Locker。
func (l *RedisLocker) Del(ctx context.Context, key string) error {
	return l.rdb.Del(ctx, key).Err()
}

type memLockEntry struct {
	expireAt time.Time
}

// MemoryLocker 是进程内 Locker 实现（无 Redis 时降级用，测试友好）。
type MemoryLocker struct {
	mu      sync.Mutex
	entries map[string]memLockEntry
}

// NewMemoryLocker 创建进程内 Locker。
func NewMemoryLocker() *MemoryLocker {
	return &MemoryLocker{entries: make(map[string]memLockEntry)}
}

// SetNX 实现 Locker。
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

// Expire 实现 Locker。key 不存在时返回 nil（与 Redis 行为差异可接受：心跳只在持锁后调用）。
func (l *MemoryLocker) Expire(_ context.Context, key string, ttl time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.entries[key]; ok {
		l.entries[key] = memLockEntry{expireAt: time.Now().Add(ttl)}
	}
	return nil
}

// Del 实现 Locker。
func (l *MemoryLocker) Del(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
	return nil
}
