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
	"strings"
	"time"

	go_redis "github.com/redis/go-redis/v9"
)

type MsgEntity struct {
	MsgID string
	Key   string
	Val   string
}

var ErrNoMsg = errors.New("no msg received")

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
	c.client = client
	return &c
}

// Close releases the underlying redis connections.
func (c *Client) Close() error {
	return c.client.Close()
}

// XADD appends a message to the stream, capping it at maxLen entries. Returns the generated message ID.
func (c *Client) XADD(ctx context.Context, topic string, maxLen int, key, val string) (string, error) {
	if topic == "" {
		return "", errors.New("redis XADD topic can't be empty")
	}

	args := &go_redis.XAddArgs{
		Stream: topic,
		ID:     "*",
		Values: map[string]any{key: val},
	}
	if maxLen > 0 {
		args.MaxLen = int64(maxLen)
	}
	return c.client.XAdd(ctx, args).Result()
}

// XACK acknowledges a message in the given consumer group.
func (c *Client) XACK(ctx context.Context, topic, groupID, msgID string) error {
	if topic == "" || groupID == "" || msgID == "" {
		return errors.New("redis XACK topic | group_id | msg_ id can't be empty")
	}

	reply, err := c.client.XAck(ctx, topic, groupID, msgID).Result()
	if err != nil {
		return err
	}
	if reply != 1 {
		return fmt.Errorf("invalid reply: %d", reply)
	}

	return nil
}

// XReadGroupPending reads messages assigned to this consumer but not yet acknowledged.
func (c *Client) XReadGroupPending(ctx context.Context, groupID, consumerID, topic string) ([]*MsgEntity, error) {
	return c.xReadGroup(ctx, groupID, consumerID, topic, 0, true)
}

// XReadGroup reads new messages from the stream for the given consumer group.
func (c *Client) XReadGroup(ctx context.Context, groupID, consumerID, topic string, timeoutMilliSeconds int) ([]*MsgEntity, error) {
	return c.xReadGroup(ctx, groupID, consumerID, topic, timeoutMilliSeconds, false)
}

func (c *Client) xReadGroup(ctx context.Context, groupID, consumerID, topic string, timeoutMilliSeconds int, pending bool) ([]*MsgEntity, error) {
	if groupID == "" || consumerID == "" || topic == "" {
		return nil, errors.New("redis XREADGROUP groupID/consumerID/topic can't be empty")
	}

	// pending=true: read messages already assigned to this consumer but not yet acked (id "0-0").
	// pending=false: read never-delivered new messages (id ">"), blocking up to timeoutMilliSeconds.
	args := &go_redis.XReadGroupArgs{
		Group:    groupID,
		Consumer: consumerID,
		Streams:  []string{topic, "0-0"},
	}
	if !pending {
		args.Streams = []string{topic, ">"}
		args.Block = time.Duration(timeoutMilliSeconds) * time.Millisecond
	}

	streams, err := c.client.XReadGroup(ctx, args).Result()
	if err != nil {
		if errors.Is(err, go_redis.Nil) {
			return nil, ErrNoMsg
		}
		return nil, err
	}
	if len(streams) == 0 {
		return nil, ErrNoMsg
	}

	var msgs []*MsgEntity
	for _, stream := range streams {
		for _, m := range stream.Messages {
			key, val := parseStreamValues(m.Values)
			msgs = append(msgs, &MsgEntity{
				MsgID: m.ID,
				Key:   key,
				Val:   val,
			})
		}
	}

	return msgs, nil
}

// parseStreamValues extracts the first field name/value pair from a stream message.
func parseStreamValues(values map[string]any) (string, string) {
	for k, v := range values {
		return k, toString(v)
	}
	return "", ""
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
	// Clamp keyCount: out of bounds values would make the capacity of args
	// below negative and panic.
	if keyCount < 0 {
		keyCount = 0
	}
	if keyCount > len(keysAndArgs) {
		keyCount = len(keysAndArgs)
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

// XGroupCreate creates a consumer group at the given start position.
func (c *Client) XGroupCreate(ctx context.Context, topic, group string) (string, error) {
	return c.client.XGroupCreate(ctx, topic, group, "0-0").Result()
}
