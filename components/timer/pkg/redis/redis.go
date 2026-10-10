package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hangtiancheng/yukino.go/components/timer/common/conf"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/log"

	go_redis "github.com/redis/go-redis/v9"
)

type Client struct {
	client go_redis.UniversalClient
}

func GetClient(confProvider *conf.RedisConfigProvider) *Client {
	config := confProvider.Get()
	client := go_redis.NewClient(&go_redis.Options{
		Addr:            config.Address,
		Password:        config.Password,
		PoolSize:        config.MaxActive,
		MinIdleConns:    config.MaxIdle,
		ConnMaxIdleTime: time.Duration(config.IdleTimeoutSeconds) * time.Second,
	})
	return &Client{client: client}
}

func (c *Client) SetEx(ctx context.Context, key, value string, expireSeconds int64) error {
	if key == "" || value == "" {
		return errors.New("redis SET key or value can't be empty")
	}
	return c.client.Set(ctx, key, value, time.Duration(expireSeconds)*time.Second).Err()
}

func (c *Client) SetNX(ctx context.Context, key, value string, expireSeconds int64) (any, error) {
	if key == "" || value == "" {
		return -1, errors.New("redis SET keyNX or value can't be empty")
	}
	ok, err := c.client.SetNX(ctx, key, value, time.Duration(expireSeconds)*time.Second).Result()
	if err != nil {
		return -1, err
	}
	if ok {
		return int64(1), nil
	}
	return int64(0), nil
}

func (c *Client) Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
	if keyCount < 0 || keyCount > len(keysAndArgs) {
		return nil, fmt.Errorf("redis Eval invalid keyCount: %d, len of keysAndArgs: %d", keyCount, len(keysAndArgs))
	}

	keys := make([]string, 0, keyCount)
	args := make([]any, 0, len(keysAndArgs)-keyCount)
	for i, v := range keysAndArgs {
		if i < keyCount {
			keys = append(keys, v.(string))
		} else {
			args = append(args, v)
		}
	}
	return c.client.Eval(ctx, src, keys, args...).Result()
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	val, err := c.client.Get(ctx, key).Result()
	if err == go_redis.Nil {
		return "", go_redis.Nil
	}
	return val, err
}

func (c *Client) Exists(ctx context.Context, keys ...string) (bool, error) {
	if len(keys) == 0 {
		return false, errors.New("redis Exists args can't be nil or empty")
	}
	n, err := c.client.Exists(ctx, keys...).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (c *Client) HGet(ctx context.Context, table, key string) (string, error) {
	val, err := c.client.HGet(ctx, table, key).Result()
	if err == go_redis.Nil {
		return "", nil
	}
	return val, err
}

func (c *Client) HSet(ctx context.Context, table, key string, value any) error {
	return c.client.HSet(ctx, table, key, value).Err()
}

func (c *Client) ZrangeByScore(ctx context.Context, table string, score1, score2 int64) ([]string, error) {
	return c.client.ZRangeArgs(ctx, go_redis.ZRangeArgs{
		Key:     table,
		Start:   score1,
		Stop:    score2,
		ByScore: true,
	}).Result()
}

func (c *Client) ZAdd(ctx context.Context, table string, score int64, value any) error {
	return c.client.ZAdd(ctx, table, go_redis.Z{Score: float64(score), Member: value}).Err()
}

func (c *Client) Expire(ctx context.Context, key string, expireSeconds int64) error {
	return c.client.Expire(ctx, key, time.Duration(expireSeconds)*time.Second).Err()
}

func NewSetCommand(args ...any) *Command {
	return &Command{
		Name: "SET",
		Args: args,
	}
}

func NewZAddCommand(args ...any) *Command {
	return &Command{
		Name: "ZADD",
		Args: args,
	}
}

func NewSetBitCommand(args ...any) *Command {
	return &Command{
		Name: "SETBIT",
		Args: args,
	}
}

func NewExpireCommand(args ...any) *Command {
	return &Command{
		Name: "EXPIRE",
		Args: args,
	}
}

type Command struct {
	Name string
	Args []any
}

func (c *Client) Transaction(ctx context.Context, commands ...*Command) ([]any, error) {
	if len(commands) == 0 {
		return nil, nil
	}

	results, err := c.client.TxPipelined(ctx, func(pipe go_redis.Pipeliner) error {
		for _, cmd := range commands {
			switch cmd.Name {
			case "SET":
				key := cmd.Args[0].(string)
				pipe.Set(ctx, key, cmd.Args[1], 0)
			case "ZADD":
				key := cmd.Args[0].(string)
				score := toInt64(cmd.Args[1])
				pipe.ZAdd(ctx, key, go_redis.Z{Score: float64(score), Member: cmd.Args[2]})
			case "SETBIT":
				key := cmd.Args[0].(string)
				offset := toInt64(cmd.Args[1])
				pipe.SetBit(ctx, key, offset, 1)
			case "EXPIRE":
				key := cmd.Args[0].(string)
				seconds := toInt64(cmd.Args[1])
				pipe.Expire(ctx, key, time.Duration(seconds)*time.Second)
			default:
				return fmt.Errorf("unsupported redis command: %s", cmd.Name)
			}
		}
		return nil
	})
	if err != nil {
		log.Errorf("redis transaction failed, err: %v", err)
		return nil, err
	}

	res := make([]any, len(results))
	for i, r := range results {
		res[i] = r
	}
	return res, nil
}

func (c *Client) SetBit(ctx context.Context, key string, offset int32) (bool, error) {
	prev, err := c.client.SetBit(ctx, key, int64(offset), 1).Result()
	if err != nil {
		return false, err
	}
	return prev == 1, nil
}

func (c *Client) GetBit(ctx context.Context, key string, offset int32) (bool, error) {
	val, err := c.client.GetBit(ctx, key, int64(offset)).Result()
	if err != nil {
		return false, err
	}
	return val == 1, nil
}

func (c *Client) MGet(ctx context.Context, keys ...string) ([]string, error) {
	if len(keys) == 0 {
		return nil, errors.New("redis MGET args can't be nil or empty")
	}
	results, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	res := make([]string, 0, len(results))
	for _, r := range results {
		if r == nil {
			res = append(res, "")
			continue
		}
		s, ok := r.(string)
		if !ok {
			res = append(res, "")
			continue
		}
		res = append(res, s)
	}
	return res, nil
}

func (c *Client) GetDistributionLock(key string) DistributeLocker {
	return NewReentrantDistributeLock(key, c)
}

func toInt64(v any) int64 {
	switch val := v.(type) {
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	default:
		return 0
	}
}

func NewUniversalClient(client go_redis.UniversalClient) *Client {
	if client == nil {
		panic("nil redis client")
	}
	return &Client{client: client}
}
