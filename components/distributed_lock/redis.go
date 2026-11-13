package distributed_lock

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	go_redis "github.com/redis/go-redis/v9"
)

type LockClient interface {
	SetNEX(ctx context.Context, key, value string, expireSeconds int64) (int64, error)
	Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error)
}

type Client struct {
	ClientOptions
	client go_redis.UniversalClient
}

func NewClient(network, address, password string, opts ...ClientOption) *Client {
	c := Client{
		ClientOptions: ClientOptions{
			network:  network,
			address:  address,
			password: password,
		},
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&c.ClientOptions)
		}
	}

	repairClient(&c.ClientOptions)

	client := go_redis.NewClient(&go_redis.Options{
		Network:         c.network,
		Addr:            c.address,
		Password:        c.password,
		PoolSize:        c.maxActive,
		MaxIdleConns:    c.maxIdle,
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

func (c *Client) SetNEX(ctx context.Context, key, value string, expireSeconds int64) (int64, error) {
	if key == "" || value == "" {
		return -1, errors.New("redis SET keyNX or value can't be empty")
	}
	if expireSeconds <= 0 {
		return -1, errors.New("redis SET NX expireSeconds must be positive")
	}
	if expireSeconds > math.MaxInt64/int64(time.Second) {
		return -1, errors.New("redis SET NX expiration exceeds time.Duration")
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

func (c *Client) Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
	if keyCount < 0 || keyCount > len(keysAndArgs) {
		return nil, fmt.Errorf("redis EVAL keyCount %d out of range [0, %d]", keyCount, len(keysAndArgs))
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
