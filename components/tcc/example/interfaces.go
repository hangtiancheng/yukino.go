package example

import (
	"context"

	"github.com/hangtiancheng/yukino.go/components/redis_lock"
)

type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) (int64, error)
	SetNX(ctx context.Context, key, value string) (int64, error)
	Del(ctx context.Context, key string) error
}

type Lock interface {
	Lock(ctx context.Context) error
	Unlock(ctx context.Context) error
}

type LockFactory func(key string, opts ...redis_lock.LockOption) Lock
