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
	client go_redis.UniversalClient
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

// NewUniversalClient shares the caller-owned connection pool, including Sentinel
// and Cluster routing. The caller is responsible for closing the pool.
func NewUniversalClient(client go_redis.UniversalClient) *Client {
	if client == nil {
		panic("nil redis client")
	}
	return &Client{client: client}
}
