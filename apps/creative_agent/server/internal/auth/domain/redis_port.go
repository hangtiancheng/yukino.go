package domain

import (
	"context"
	"time"
)

// RedisPort abstracts the Redis primitives needed for login-failure counting and the token blacklist.
//
// It exposes only the operations Login/Logout need; the application layer depends on this interface instead of *redis.Client,
// It eases test mocking and no-Redis degradation. See infrastructure.RedisAdapter for the implementation.
type RedisPort interface {
	// Get reads a key value; returns redis.Nil when the key does not exist.
	Get(ctx context.Context, key string) (string, error)
	// Set writes a key value with a TTL.
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	// Del deletes a key.
	Del(ctx context.Context, key string) error
	// Incr increments and returns the new value (starting from 1 when the key does not exist).
	Incr(ctx context.Context, key string) (int64, error)
	// Expire sets the key's expiry.
	Expire(ctx context.Context, key string, ttl time.Duration) error
	// Exists checks whether keys exist, returning the count of existing keys.
	Exists(ctx context.Context, key string) (int64, error)
}
