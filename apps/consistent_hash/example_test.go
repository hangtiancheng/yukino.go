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

package consistent_hash

import (
	"context"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/consistent_hash/local"
	"github.com/hangtiancheng/yukino.go/apps/consistent_hash/redis"
)

func Test_local_consistent_hash(t *testing.T) {
	localHashRing := local.NewSkiplistHashRing()
	hasher := NewFnvHasher()
	localMigrator := func(ctx context.Context, dataKeys map[string]struct{}, from, to string) error {
		t.Logf("from: %s, to: %s, data keys: %v", from, to, dataKeys)
		return nil
	}
	consistentHash := NewConsistentHash(
		localHashRing,
		hasher,
		localMigrator,
		// Virtual nodes per node = weight * replicas.
		WithReplicas(5),
		// The hash-ring lock auto-releases after 5 seconds.
		WithLockExpireSeconds(5),
	)
	test(t, consistentHash)
}

const (
	network  = "tcp"
	address  = "redis address"
	password = "redis password"

	hashRingKey = "hash ring unique id"
)

func Test_redis_consistent_hash(t *testing.T) {
	if address == "redis address" {
		t.Skip("fill in a real redis address to run this test")
	}
	redisClient := redis.NewClient(network, address, password)
	hashRing := redis.NewRedisHashRing(hashRingKey, redisClient)
	consistentHash := NewConsistentHash(hashRing, NewFnvHasher(), nil)
	test(t, consistentHash)
}

func test(t *testing.T, consistentHash *ConsistentHash) {
	ctx := context.Background()
	nodeA := "node_a"
	weightNodeA := 2
	nodeB := "node_b"
	weightNodeB := 1
	nodeC := "node_c"
	weightNodeC := 1
	if err := consistentHash.AddNode(ctx, nodeA, weightNodeA); err != nil {
		t.Error(err)
		return
	}

	if err := consistentHash.AddNode(ctx, nodeB, weightNodeB); err != nil {
		t.Error(err)
		return
	}

	dataKeyA := "data_a"
	dataKeyB := "data_b"
	dataKeyC := "data_c"
	dataKeyD := "data_d"
	node, err := consistentHash.GetNode(ctx, dataKeyA)
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyA, node)
	if node, err = consistentHash.GetNode(ctx, dataKeyB); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyB, node)
	if node, err = consistentHash.GetNode(ctx, dataKeyC); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyC, node)
	if node, err = consistentHash.GetNode(ctx, dataKeyD); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyD, node)
	if err := consistentHash.AddNode(ctx, nodeC, weightNodeC); err != nil {
		t.Error(err)
		return
	}
	if node, err = consistentHash.GetNode(ctx, dataKeyA); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyA, node)
	if node, err = consistentHash.GetNode(ctx, dataKeyB); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyB, node)
	if node, err = consistentHash.GetNode(ctx, dataKeyC); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyC, node)
	if node, err = consistentHash.GetNode(ctx, dataKeyD); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyD, node)
	if err = consistentHash.RemoveNode(ctx, nodeC); err != nil {
		t.Error(err)
		return
	}
	if node, err = consistentHash.GetNode(ctx, dataKeyA); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyA, node)
	if node, err = consistentHash.GetNode(ctx, dataKeyB); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyB, node)
	if node, err = consistentHash.GetNode(ctx, dataKeyC); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyC, node)
	if node, err = consistentHash.GetNode(ctx, dataKeyD); err != nil {
		t.Error(err)
		return
	}
	t.Logf("data: %s belongs to node: %s", dataKeyD, node)
	t.Log("ok")
}

func Test_local_lock(t *testing.T) {
	hashRing := local.NewSkiplistHashRing()
	ctx := context.Background()
	if err := hashRing.Lock(ctx, 1); err != nil {
		t.Error(err)
		return
	}
	<-time.After(2 * time.Second)
	if err := hashRing.Lock(ctx, 2); err != nil {
		t.Error(err)
		return
	}
	if err := hashRing.Unlock(ctx); err != nil {
		t.Error(err)
		return
	}
}
