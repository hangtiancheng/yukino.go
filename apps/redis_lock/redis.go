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

package redis_lock

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	go_redis "github.com/redis/go-redis/v9"
)

// LockClient is the redis surface used by RedisLock.
type LockClient interface {
	SetNEX(ctx context.Context, key, value string, expireSeconds int64) (int64, error)
	Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error)
}

// Client wraps a github.com/redis/go-redis/v9 client.
type Client struct {
	ClientOptions
	client *go_redis.Client
}

// NewClient builds a Client against the given redis endpoint.
func NewClient(network, address, password string, opts ...ClientOption) *Client {
	c := Client{
		ClientOptions: ClientOptions{
			network:  network,
			address:  address,
			password: password,
		},
	}

	for _, opt := range opts {
		opt(&c.ClientOptions)
	}

	repairClient(&c.ClientOptions)

	client := go_redis.NewClient(&go_redis.Options{
		Network:         c.network,
		Addr:            c.address,
		Password:        c.password,
		PoolSize:        c.maxActive,
		MinIdleConns:    c.maxIdle,
		ConnMaxIdleTime: time.Duration(c.idleTimeoutSeconds) * time.Second,
	})
	return &Client{
		ClientOptions: c.ClientOptions,
		client:        client,
	}
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", errors.New("redis GET key can't be empty")
	}
	return c.client.Get(ctx, key).Result()
}

func (c *Client) Set(ctx context.Context, key, value string) (int64, error) {
	if key == "" || value == "" {
		return -1, errors.New("redis SET key or value can't be empty")
	}

	resp, err := c.client.Set(ctx, key, value, 0).Result()
	if err != nil {
		return -1, err
	}
	if strings.ToLower(resp) == "ok" {
		return 1, nil
	}
	return 0, nil
}

// SetNEX runs SET key value NX EX expireSeconds. Returns 1 on success, 0 if the key already exists.
func (c *Client) SetNEX(ctx context.Context, key, value string, expireSeconds int64) (int64, error) {
	if key == "" || value == "" {
		return -1, errors.New("redis SET keyNX or value can't be empty")
	}
	// expireSeconds <= 0 would create a key that never expires (or is rejected),
	// leaving a lock stuck forever after the holder crashes.
	if expireSeconds <= 0 {
		return -1, errors.New("redis SET NX expireSeconds must be positive")
	}

	ok, err := c.client.SetNX(ctx, key, value, time.Duration(expireSeconds)*time.Second).Result()
	if err != nil {
		return -1, err
	}
	if ok {
		return 1, nil
	}
	return 0, nil
}

func (c *Client) SetNX(ctx context.Context, key, value string) (int64, error) {
	if key == "" || value == "" {
		return -1, errors.New("redis SET key NX or value can't be empty")
	}

	ok, err := c.client.SetNX(ctx, key, value, 0).Result()
	if err != nil {
		return -1, err
	}
	if ok {
		return 1, nil
	}
	return 0, nil
}

func (c *Client) Del(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("redis DEL key can't be empty")
	}
	return c.client.Del(ctx, key).Err()
}

func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	if key == "" {
		return -1, errors.New("redis INCR key can't be empty")
	}
	return c.client.Incr(ctx, key).Result()
}

// Eval runs the given Lua script. The first keyCount entries of keysAndArgs are KEYS, the rest are ARGV.
func (c *Client) Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
	// Guard against keyCount out of range: a negative capacity below would panic.
	if keyCount < 0 || keyCount > len(keysAndArgs) {
		return nil, fmt.Errorf("redis EVAL keyCount %d out of range [0, %d]", keyCount, len(keysAndArgs))
	}
	keys := make([]string, 0, keyCount)
	args := make([]any, 0, len(keysAndArgs)-keyCount)
	for i, v := range keysAndArgs {
		if i < keyCount {
			keys = append(keys, fmt.Sprintf("%v", v))
		} else {
			args = append(args, v)
		}
	}
	return c.client.Eval(ctx, src, keys, args...).Result()
}
