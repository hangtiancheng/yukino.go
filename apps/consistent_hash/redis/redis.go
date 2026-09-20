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
	"time"

	go_redis "github.com/redis/go-redis/v9"
)

var ErrScoreNotExist = errors.New("score not exist")

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

// ZAdd runs the Redis ZADD command.
func (c *Client) ZAdd(ctx context.Context, table string, score int64, value string) error {
	return c.client.ZAdd(ctx, table, go_redis.Z{Score: float64(score), Member: value}).Err()
}

type ScoreEntity struct {
	Score int64
	Val   string
}

// ZRangeByScore runs the Redis ZRANGE ... BYSCORE WITHSCORES command.
func (c *Client) ZRangeByScore(ctx context.Context, table string, score1, score2 int64) ([]*ScoreEntity, error) {
	members, err := c.client.ZRangeArgsWithScores(ctx, go_redis.ZRangeArgs{
		Key:     table,
		Start:   score1,
		Stop:    score2,
		ByScore: true,
	}).Result()
	if err != nil {
		return nil, err
	}

	scoreEntities := make([]*ScoreEntity, 0, len(members))
	for _, m := range members {
		scoreEntities = append(scoreEntities, &ScoreEntity{
			Score: int64(m.Score),
			Val:   toString(m.Member),
		})
	}

	return scoreEntities, nil
}

// Ceiling returns the first member with score >= the given score.
func (c *Client) Ceiling(ctx context.Context, table string, score int64) (*ScoreEntity, error) {
	members, err := c.client.ZRangeArgsWithScores(ctx, go_redis.ZRangeArgs{
		Key:     table,
		Start:   score,
		Stop:    "+inf",
		ByScore: true,
		Offset:  0,
		Count:   1,
	}).Result()
	if err != nil {
		return nil, err
	}

	if len(members) != 1 {
		return nil, fmt.Errorf("invalid len of entity: %d, err: %w", len(members), ErrScoreNotExist)
	}

	return &ScoreEntity{
		Score: int64(members[0].Score),
		Val:   toString(members[0].Member),
	}, nil
}

// Floor returns the first member with score <= the given score (descending).
func (c *Client) Floor(ctx context.Context, table string, score int64) (*ScoreEntity, error) {
	members, err := c.client.ZRangeArgsWithScores(ctx, go_redis.ZRangeArgs{
		Key:     table,
		Start:   score,
		Stop:    "-inf",
		ByScore: true,
		Rev:     true,
		Offset:  0,
		Count:   1,
	}).Result()
	if err != nil {
		return nil, err
	}

	if len(members) != 1 {
		return nil, fmt.Errorf("invalid len of entity: %d, err: %w", len(members), ErrScoreNotExist)
	}

	return &ScoreEntity{
		Score: int64(members[0].Score),
		Val:   toString(members[0].Member),
	}, nil
}

// FirstOrLast returns the member with the smallest (first=true) or largest (first=false) score.
func (c *Client) FirstOrLast(ctx context.Context, table string, first bool) (*ScoreEntity, error) {
	args := go_redis.ZRangeArgs{
		Key:     table,
		Start:   "-inf",
		Stop:    "+inf",
		ByScore: true,
		Offset:  0,
		Count:   1,
	}
	if !first {
		args.Start = "+inf"
		args.Stop = "-inf"
		args.Rev = true
	}

	members, err := c.client.ZRangeArgsWithScores(ctx, args).Result()
	if err != nil {
		return nil, err
	}

	if len(members) != 1 {
		return nil, fmt.Errorf("invalid len of entity: %d, err: %w", len(members), ErrScoreNotExist)
	}

	return &ScoreEntity{
		Score: int64(members[0].Score),
		Val:   toString(members[0].Member),
	}, nil
}

func (c *Client) ZRem(ctx context.Context, table string, score int64) error {
	scoreStr := fmt.Sprintf("%d", score)
	return c.client.ZRemRangeByScore(ctx, table, scoreStr, scoreStr).Err()
}

func (c *Client) HSet(ctx context.Context, table, key, val string) error {
	return c.client.HSet(ctx, table, key, val).Err()
}

func (c *Client) HGetAll(ctx context.Context, table string) (map[string]string, error) {
	return c.client.HGetAll(ctx, table).Result()
}

func (c *Client) HDel(ctx context.Context, table, key string) error {
	return c.client.HDel(ctx, table, key).Err()
}

func (c *Client) Set(ctx context.Context, key, val string) error {
	return c.client.Set(ctx, key, val, 0).Err()
}

func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.client.Get(ctx, key).Result()
}

func (c *Client) Del(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

// Eval runs the given Lua script. The first keyCount entries of keysAndArgs are KEYS, the rest are ARGV.
func (c *Client) Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
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

// SetNEX runs SET key value NX EX expireSeconds. Returns 1 on success, 0 if the key already exists.
func (c *Client) SetNEX(ctx context.Context, key, value string, expireSeconds int64) (int64, error) {
	if key == "" || value == "" {
		return -1, errors.New("redis SET keyNX or value can't be empty")
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

func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}
