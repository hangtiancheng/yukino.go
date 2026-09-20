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
	"fmt"
	"time"

	go_redis "github.com/redis/go-redis/v9"
)

// Client wraps a github.com/redis/go-redis/v9 client.
type Client struct {
	opts   *ClientOptions
	client *go_redis.Client
}

func NewClient(network, address, password string, opts ...ClientOption) *Client {
	c := Client{
		opts: &ClientOptions{
			network:  network,
			address:  address,
			password: password,
		},
	}

	for _, opt := range opts {
		opt(c.opts)
	}

	repairClient(c.opts)

	client := go_redis.NewClient(&go_redis.Options{
		Network:         c.opts.network,
		Addr:            c.opts.address,
		Password:        c.opts.password,
		PoolSize:        c.opts.maxActive,
		MinIdleConns:    c.opts.maxIdle,
		ConnMaxIdleTime: time.Duration(c.opts.idleTimeoutSeconds) * time.Second,
	})
	return &Client{
		client: client,
	}
}

func (c *Client) SAdd(ctx context.Context, key, val string) (int, error) {
	n, err := c.client.SAdd(ctx, key, val).Result()
	return int(n), err
}

// Eval runs the given Lua script. The first keyCount entries of keysAndArgs are KEYS, the rest are ARGV.
func (c *Client) Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
	keys := make([]string, 0, keyCount)
	argsLen := len(keysAndArgs) - keyCount
	if argsLen < 0 {
		argsLen = 0
	}
	args := make([]any, 0, argsLen)
	for i, v := range keysAndArgs {
		if i < keyCount {
			keys = append(keys, fmt.Sprintf("%v", v))
		} else {
			args = append(args, v)
		}
	}
	return c.client.Eval(ctx, src, keys, args...).Result()
}
