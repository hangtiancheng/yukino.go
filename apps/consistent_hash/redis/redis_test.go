package redis

import (
	"context"
	"testing"
)

const (
	network  = "tcp"
	address  = "please fill in redis address {ip}:{port}"
	password = "please fill in redis password, empty string if none"
)

// skipWithoutRedis skips the test when the redis address is still the placeholder.
func skipWithoutRedis(t *testing.T) {
	t.Helper()
	if address == "please fill in redis address {ip}:{port}" {
		t.Skip("fill in a real redis address to run this test")
	}
}

func Test_ZRangeByScore(t *testing.T) {
	skipWithoutRedis(t)
	client := NewClient(network, address, password)
	ctx := context.Background()
	scoreEntity, err := client.ZRangeByScore(ctx, "my_zset", 1, 1)
	if err != nil {
		t.Error(err)
		return
	}
	t.Errorf("score: %v", scoreEntity)
}

func Test_Ceiling(t *testing.T) {
	skipWithoutRedis(t)
	client := NewClient(network, address, password)
	ctx := context.Background()
	scoreEntity, err := client.Ceiling(ctx, "my_zset", 3)
	if err != nil {
		t.Error(err)
		return
	}
	t.Errorf("scoreEntity: %+v", scoreEntity)
}

func Test_Floor(t *testing.T) {
	skipWithoutRedis(t)
	client := NewClient(network, address, password)
	ctx := context.Background()
	scoreEntity, err := client.Floor(ctx, "my_zset", 0)
	if err != nil {
		t.Error(err)
		return
	}
	t.Errorf("scoreEntity: %+v", scoreEntity)
}

func Test_Last(t *testing.T) {
	skipWithoutRedis(t)
	client := NewClient(network, address, password)
	ctx := context.Background()
	scoreEntity, err := client.FirstOrLast(ctx, "my_zset", false)
	if err != nil {
		t.Error(err)
		return
	}
	t.Errorf("scoreEntity: %+v", scoreEntity)
}

func Test_HGetAll(t *testing.T) {
	skipWithoutRedis(t)
	client := NewClient(network, address, password)
	ctx := context.Background()
	res, err := client.HGetAll(ctx, "my_hset")
	if err != nil {
		t.Error(err)
		return
	}
	t.Error(res)
}
