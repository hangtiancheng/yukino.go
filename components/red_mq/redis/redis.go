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
	c.client = client
	return &c
}

func (c *Client) Close() error {
	return c.client.Close()
}

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

func (c *Client) XReadGroupPending(ctx context.Context, groupID, consumerID, topic string) ([]*MsgEntity, error) {
	return c.xReadGroup(ctx, groupID, consumerID, topic, 0, true)
}

func (c *Client) XReadGroup(ctx context.Context, groupID, consumerID, topic string, timeoutMilliSeconds int) ([]*MsgEntity, error) {
	return c.xReadGroup(ctx, groupID, consumerID, topic, timeoutMilliSeconds, false)
}

func (c *Client) xReadGroup(ctx context.Context, groupID, consumerID, topic string, timeoutMilliSeconds int, pending bool) ([]*MsgEntity, error) {
	if groupID == "" || consumerID == "" || topic == "" {
		return nil, errors.New("redis XREADGROUP groupID/consumerID/topic can't be empty")
	}

	args := &go_redis.XReadGroupArgs{
		Group:    groupID,
		Count:    1,
		Consumer: consumerID,
		Streams:  []string{topic, "0-0"},
	}
	if !pending {
		args.Streams = []string{topic, ">"}
		args.Count = 1
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

func (c *Client) Eval(ctx context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
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

func (c *Client) XGroupCreate(ctx context.Context, topic, group string) error {
	err := c.client.XGroupCreateMkStream(ctx, topic, group, "0-0").Err()
	if err != nil && strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func NewUniversalClient(client go_redis.UniversalClient) *Client {
	if client == nil {
		panic("nil redis client")
	}
	return &Client{client: client}
}

func (c *Client) XAutoClaim(ctx context.Context, topic, group, consumer, start string, idle time.Duration) ([]*MsgEntity, string, error) {
	messages, next, err := c.client.XAutoClaim(ctx, &go_redis.XAutoClaimArgs{Stream: topic, Group: group, Consumer: consumer, Start: start, MinIdle: idle, Count: 1}).Result()
	if err != nil {
		return nil, "0-0", err
	}
	result := make([]*MsgEntity, 0, len(messages))
	for _, m := range messages {
		key, val := parseStreamValues(m.Values)
		result = append(result, &MsgEntity{MsgID: m.ID, Key: key, Val: val})
	}
	return result, next, nil
}
