package example

import (
	"context"

	"github.com/hangtiancheng/yukino.go/components/redis_lock"
)

// RedisClient abstracts the redis operations used by TCC components.
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) (int64, error)
	SetNX(ctx context.Context, key, value string) (int64, error)
	Del(ctx context.Context, key string) error
}

// Lock abstracts a distributed lock.
type Lock interface {
	Lock(ctx context.Context) error
	Unlock(ctx context.Context) error
}

// LockFactory creates a Lock for the given key.
type LockFactory func(key string, opts ...redis_lock.LockOption) Lock
