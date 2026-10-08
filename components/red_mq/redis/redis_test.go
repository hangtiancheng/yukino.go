package redis

import (
	"context"
	"testing"
)

const (
	network  = "tcp"
	address  = "please fill in redis address"
	password = "please fill in redis password"
)

// skipWithoutRedis skips the integration tests until the connection
// constants above are filled in with a real redis.
func skipWithoutRedis(t *testing.T) {
	t.Helper()
	if address == "please fill in redis address" {
		t.Skip("redis address not configured, fill in the placeholders to run the integration tests")
	}
}

func Test_redis_xadd(t *testing.T) {
	skipWithoutRedis(t)
	client := NewClient(network, address, password)
	defer client.Close()
	ctx := context.Background()
	res, err := client.XADD(ctx, "test_stream_topic", 3, "test_key", "test_val")
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(res)
}

func Test_redis_xReaderGroup(t *testing.T) {
	skipWithoutRedis(t)
	client := NewClient(network, address, password)
	defer client.Close()
	ctx := context.Background()
	res, err := client.XReadGroupPending(ctx, "my_group_4", "my_consumer", "stream_topic")
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(res)
}
