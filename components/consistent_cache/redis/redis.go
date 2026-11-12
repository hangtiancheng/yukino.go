package redis

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	go_redis "github.com/redis/go-redis/v9"
)

type Config struct {
	Address             string
	Password            string
	DB                  int
	PoolSize            int
	MinIdleConns        int
	ConnMaxIdleSeconds  int
	DialTimeoutSeconds  int
	ReadTimeoutSeconds  int
	WriteTimeoutSeconds int
}

type RClient struct {
	client go_redis.UniversalClient
}

func NewRClient(config *Config) *RClient {
	return &RClient{client: getRedisClient(config)}
}

func getRedisClient(config *Config) *go_redis.Client {
	if config == nil || config.Address == "" {
		panic("redis address is required")
	}

	opts := &go_redis.Options{
		Addr:         config.Address,
		Password:     config.Password,
		DB:           config.DB,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
	}
	if config.ConnMaxIdleSeconds > 0 {
		opts.ConnMaxIdleTime = time.Duration(config.ConnMaxIdleSeconds) * time.Second
	}
	if config.DialTimeoutSeconds > 0 {
		opts.DialTimeout = time.Duration(config.DialTimeoutSeconds) * time.Second
	}
	if config.ReadTimeoutSeconds > 0 {
		opts.ReadTimeout = time.Duration(config.ReadTimeoutSeconds) * time.Second
	}
	if config.WriteTimeoutSeconds > 0 {
		opts.WriteTimeout = time.Duration(config.WriteTimeoutSeconds) * time.Second
	}
	return go_redis.NewClient(opts)
}

func (r *RClient) Get(ctx context.Context, key string) (string, error) {
	if r == nil || r.client == nil || ctx == nil {
		return "", errors.New("redis client and context are required")
	}
	if key == "" {
		return "", errors.New("redis GET key can't be empty")
	}

	val, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, go_redis.Nil) {
		return "", ErrorCacheMiss
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (r *RClient) SetEx(ctx context.Context, key, value string, expireSeconds int64) error {
	if key == "" {
		return errors.New("redis SET EX key can't be empty")
	}
	if expireSeconds <= 0 || expireSeconds > math.MaxInt64/int64(time.Second) {
		return errors.New("redis SET EX expiration is out of range")
	}
	if r == nil || r.client == nil || ctx == nil {
		return errors.New("redis client and context are required")
	}

	return r.client.Set(ctx, key, value, time.Duration(expireSeconds)*time.Second).Err()
}

func (r *RClient) Del(ctx context.Context, key string) error {
	if r == nil || r.client == nil || ctx == nil {
		return errors.New("redis client and context are required")
	}
	if key == "" {
		return errors.New("redis DEL key can't be empty")
	}

	return r.client.Del(ctx, key).Err()
}

func (r *RClient) Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
	if r == nil || r.client == nil || ctx == nil {
		return nil, errors.New("redis client and context are required")
	}
	if keyCount < 0 || keyCount > len(keysAndArgs) {
		return nil, fmt.Errorf("redis EVAL key count %d out of range [0, %d]", keyCount, len(keysAndArgs))
	}
	argCount := len(keysAndArgs) - keyCount
	keys := make([]string, 0, keyCount)
	args := make([]any, 0, argCount)
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
	return r.client.Eval(ctx, src, keys, args...).Result()
}

func (r *RClient) PExpire(ctx context.Context, key string, expireMillis int64) error {
	if key == "" {
		return errors.New("redis PEXPIRE key can't be empty")
	}
	if expireMillis <= 0 || expireMillis > math.MaxInt64/int64(time.Millisecond) {
		return errors.New("redis PEXPIRE expiration is out of range")
	}
	if r == nil || r.client == nil || ctx == nil {
		return errors.New("redis client and context are required")
	}

	return r.client.PExpire(ctx, key, time.Duration(expireMillis)*time.Millisecond).Err()
}

var ErrorCacheMiss = errors.New("redis cache miss")

func NewUniversalClient(client go_redis.UniversalClient) *RClient {
	if client == nil {
		panic("nil redis client")
	}
	return &RClient{client: client}
}

func (r *RClient) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}
