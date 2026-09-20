// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package redis

import (
	"context"
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/consistent_cache"
)

// Client abstracts the redis commands used by Cache.
type Client interface {
	Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error)
	Get(ctx context.Context, key string) (string, error)
	SetEx(ctx context.Context, key, value string, expireSeconds int64) error
	Del(ctx context.Context, key string) error
	PExpire(ctx context.Context, key string, expireMillis int64) error
}

// Cache implements consistent_cache.Cache backed by redis.
type Cache struct {
	client Client
}

// NewRedisCache builds a redis-backed Cache.
func NewRedisCache(config *Config) *Cache {
	return &Cache{client: NewRClient(config)}
}

// Enable re-enables the read-path write cache for a key by expiring the disable marker shortly.
// The disable marker is considered absent once it expires, which means the read path is enabled.
func (c *Cache) Enable(ctx context.Context, key string, delayMillis int64) error {
	return c.client.PExpire(ctx, c.disableKey(key), delayMillis)
}

// Disable turns off the read-path write cache for a key by setting a short-lived disable marker.
func (c *Cache) Disable(ctx context.Context, key string, expireSeconds int64) error {
	return c.client.SetEx(ctx, c.disableKey(key), "1", expireSeconds)
}

// Get reads the cached value for the key.
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

// PutWhenEnable writes the cache only if the read-path write cache is enabled (i.e. disable marker absent).
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

// Del removes the cached value for the key.
func (c *Cache) Del(ctx context.Context, key string) error {
	return c.client.Del(ctx, key)
}

// disableKey derives the disable-marker key. The {%s} hash tag keeps key and disable key
// on the same slot in redis cluster mode.
func (c *Cache) disableKey(key string) string {
	return fmt.Sprintf("Enable_Lock_Key_{%s}", key)
}
