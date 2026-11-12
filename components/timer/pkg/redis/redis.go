package redis

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	expiration, err := secondsDuration(expireSeconds)
	if err != nil {
		return err
	}
	if c == nil || c.client == nil || ctx == nil {
		return errors.New("redis client and context are required")
	}
	return c.client.Set(ctx, key, value, expiration).Err()
}

func (c *Client) SetNX(ctx context.Context, key, value string, expireSeconds int64) (any, error) {
	if key == "" || value == "" {
		return -1, errors.New("redis SET keyNX or value can't be empty")
	}
	expiration, err := secondsDuration(expireSeconds)
	if err != nil {
		return -1, err
	}
	if c == nil || c.client == nil || ctx == nil {
		return -1, errors.New("redis client and context are required")
	}
	ok, err := c.client.SetNX(ctx, key, value, expiration).Result()
	if err != nil {
		return -1, err
	}
	if ok {
		return int64(1), nil
	}
	return int64(0), nil
}

func (c *Client) Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
	if c == nil || c.client == nil || ctx == nil {
		return nil, errors.New("redis client and context are required")
	}
	if keyCount < 0 || keyCount > len(keysAndArgs) {
		return nil, fmt.Errorf("redis Eval invalid keyCount: %d, len of keysAndArgs: %d", keyCount, len(keysAndArgs))
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
	if key == "" {
		return errors.New("redis EXPIRE requires a key")
	}
	expiration, err := secondsDuration(expireSeconds)
	if err != nil {
		return err
	}
	if c == nil || c.client == nil || ctx == nil {
		return errors.New("redis client and context are required")
	}
	return c.client.Expire(ctx, key, expiration).Err()
}

func secondsDuration(seconds int64) (time.Duration, error) {
	if seconds <= 0 || seconds > math.MaxInt64/int64(time.Second) {
		return 0, errors.New("redis expiration must be positive and fit in time.Duration")
	}
	return time.Duration(seconds) * time.Second, nil
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
	if c == nil || c.client == nil {
		return nil, errors.New("redis client is nil")
	}
	if ctx == nil {
		return nil, errors.New("redis transaction context is nil")
	}
	if len(commands) == 0 {
		return nil, nil
	}
	for i, cmd := range commands {
		if err := validateCommand(cmd); err != nil {
			return nil, fmt.Errorf("invalid redis command %d: %w", i, err)
		}
	}

	results, err := c.client.TxPipelined(ctx, func(pipe go_redis.Pipeliner) error {
		for _, cmd := range commands {
			switch cmd.Name {
			case "SET":
				key := cmd.Args[0].(string)
				pipe.Set(ctx, key, cmd.Args[1], 0)
			case "ZADD":
				key := cmd.Args[0].(string)
				score, _ := toInt64(cmd.Args[1])
				pipe.ZAdd(ctx, key, go_redis.Z{Score: float64(score), Member: cmd.Args[2]})
			case "SETBIT":
				key := cmd.Args[0].(string)
				offset, _ := toInt64(cmd.Args[1])
				pipe.SetBit(ctx, key, offset, 1)
			case "EXPIRE":
				key := cmd.Args[0].(string)
				seconds, _ := toInt64(cmd.Args[1])
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

func (c *Client) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
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

func validateCommand(cmd *Command) error {
	if cmd == nil {
		return errors.New("command is nil")
	}
	var key string
	var expectedArgs int
	switch cmd.Name {
	case "SET":
		expectedArgs = 2
	case "ZADD":
		expectedArgs = 3
	case "SETBIT", "EXPIRE":
		expectedArgs = 2
	default:
		return fmt.Errorf("unsupported redis command: %s", cmd.Name)
	}
	if len(cmd.Args) != expectedArgs {
		return fmt.Errorf("%s expects %d arguments, got %d", cmd.Name, expectedArgs, len(cmd.Args))
	}
	var ok bool
	if key, ok = cmd.Args[0].(string); !ok || key == "" {
		return fmt.Errorf("%s key must be a non-empty string", cmd.Name)
	}
	if cmd.Name == "SET" {
		if cmd.Args[1] == nil {
			return errors.New("SET value is nil")
		}
		return nil
	}
	valueIndex := 1
	if cmd.Name == "ZADD" {
		valueIndex = 2
		if cmd.Args[valueIndex] == nil {
			return errors.New("ZADD member is nil")
		}
	}
	value, err := toInt64(cmd.Args[1])
	if err != nil {
		return err
	}
	switch cmd.Name {
	case "SETBIT":
		if value < 0 {
			return errors.New("SETBIT offset must not be negative")
		}
	case "EXPIRE":
		if value <= 0 || value > math.MaxInt64/int64(time.Second) {
			return errors.New("EXPIRE duration is out of range")
		}
	}
	return nil
}

func toInt64(v any) (int64, error) {
	switch val := v.(type) {
	case int:
		return int64(val), nil
	case int8:
		return int64(val), nil
	case int16:
		return int64(val), nil
	case int32:
		return int64(val), nil
	case int64:
		return val, nil
	case uint:
		if uint64(val) > math.MaxInt64 {
			return 0, errors.New("integer argument exceeds int64 range")
		}
		return int64(val), nil
	case uint8:
		return int64(val), nil
	case uint16:
		return int64(val), nil
	case uint32:
		return int64(val), nil
	case uint64:
		if val > math.MaxInt64 {
			return 0, errors.New("integer argument exceeds int64 range")
		}
		return int64(val), nil
	default:
		return 0, fmt.Errorf("integer argument has unsupported type %T", v)
	}
}

func NewUniversalClient(client go_redis.UniversalClient) *Client {
	if client == nil {
		panic("nil redis client")
	}
	return &Client{client: client}
}
