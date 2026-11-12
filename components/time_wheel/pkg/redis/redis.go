package redis

import (
	"context"
	"fmt"
	"time"

	go_redis "github.com/redis/go-redis/v9"
)

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
		if opt != nil {
			opt(c.opts)
		}
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

func (c *Client) Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}
	if ctx == nil {
		return nil, fmt.Errorf("redis context is nil")
	}
	if keyCount < 0 || keyCount > len(keysAndArgs) {
		return nil, fmt.Errorf("redis EVAL key count %d out of range [0, %d]", keyCount, len(keysAndArgs))
	}
	keys := make([]string, 0, keyCount)
	args := make([]any, 0, len(keysAndArgs)-keyCount)
	for i, v := range keysAndArgs {
		if i < keyCount {
			key, ok := v.(string)
			if !ok || key == "" {
				return nil, fmt.Errorf("redis EVAL key %d must be a non-empty string", i)
			}
			keys = append(keys, key)
		} else {
			args = append(args, v)
		}
	}
	return c.client.Eval(ctx, src, keys, args...).Result()
}

func NewUniversalClient(client go_redis.UniversalClient) *Client {
	if client == nil {
		panic("nil redis client")
	}
	return &Client{client: client}
}

func (c *Client) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}
