package redis

import (
	"context"
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino.go/components/consistent_cache"
)

type Client interface {
	Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error)
	Get(ctx context.Context, key string) (string, error)
	SetEx(ctx context.Context, key, value string, expireSeconds int64) error
	Del(ctx context.Context, key string) error
	PExpire(ctx context.Context, key string, expireMillis int64) error
}

type Cache struct {
	client Client
}

func NewRedisCache(config *Config) *Cache {
	return &Cache{client: NewRClient(config)}
}

func NewCache(client Client) *Cache { return &Cache{client: client} }

func (c *Cache) Enable(ctx context.Context, key string, delayMillis int64) error {
	return c.client.PExpire(ctx, c.disableKey(key), delayMillis)
}

func (c *Cache) Disable(ctx context.Context, key string, expireSeconds int64) error {
	return c.client.SetEx(ctx, c.disableKey(key), "1", expireSeconds)
}

func (c *Cache) Get(ctx context.Context, key string) (string, error) {
	reply, err := c.client.Get(ctx, key)
	if err != nil && !errors.Is(err, ErrorCacheMiss) {
		return "", err
	}
	if errors.Is(err, ErrorCacheMiss) {
		return "", consistent_cache.ErrorCacheMiss
	}
	return reply, nil
}

func (c *Cache) PutWhenEnable(ctx context.Context, key, value string, expireSeconds int64) (bool, error) {
	reply, err := c.client.Eval(ctx, LuaCheckEnableAndWriteCache, 2, []any{
		c.disableKey(key),
		key,
		value,
		expireSeconds,
	})
	if err != nil {
		return false, err
	}
	n, ok := reply.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected lua reply type: %T", reply)
	}
	return n == 1, nil
}

func (c *Cache) Del(ctx context.Context, key string) error {
	return c.client.Del(ctx, key)
}

func (c *Cache) disableKey(key string) string {
	return fmt.Sprintf("Enable_Lock_Key_{%s}", key)
}
