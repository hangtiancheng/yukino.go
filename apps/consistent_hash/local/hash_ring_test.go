package local

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSkiplistHashRing_ceiling_floor(t *testing.T) {
	ring := NewSkiplistHashRing()
	ctx := context.Background()

	// Empty ring: both queries report the -1 sentinel.
	if score, _ := ring.Ceiling(ctx, 0); score != -1 {
		t.Fatalf("empty ring Ceiling = %d, want -1", score)
	}
	if score, _ := ring.Floor(ctx, 0); score != -1 {
		t.Fatalf("empty ring Floor = %d, want -1", score)
	}

	for score, node := range map[int32]string{100: "a", 300: "b", 500: "c"} {
		if err := ring.Add(ctx, score, node); err != nil {
			t.Fatal(err)
		}
	}

	// Ceiling: exact hit, in-between, and wrap-around past the largest score.
	for _, tc := range []struct{ query, want int32 }{
		{100, 100}, {200, 300}, {500, 500}, {501, 100},
	} {
		if score, _ := ring.Ceiling(ctx, tc.query); score != tc.want {
			t.Fatalf("Ceiling(%d) = %d, want %d", tc.query, score, tc.want)
		}
	}

	// Floor: exact hit, in-between, and wrap-around below the smallest score.
	for _, tc := range []struct{ query, want int32 }{
		{100, 100}, {200, 100}, {400, 300}, {500, 500}, {50, 500},
	} {
		if score, _ := ring.Floor(ctx, tc.query); score != tc.want {
			t.Fatalf("Floor(%d) = %d, want %d", tc.query, score, tc.want)
		}
	}

	if nodes, _ := ring.Node(ctx, 300); len(nodes) != 1 || nodes[0] != "b" {
		t.Fatalf("Node(300) = %v, want [b]", nodes)
	}
	if _, err := ring.Node(ctx, 999); err == nil {
		t.Fatal("Node on a missing score should fail")
	}

	// Two node ids may share one score.
	if err := ring.Add(ctx, 300, "b2"); err != nil {
		t.Fatal(err)
	}
	if nodes, _ := ring.Node(ctx, 300); len(nodes) != 2 {
		t.Fatalf("Node(300) = %v, want [b b2]", nodes)
	}
	if err := ring.Rem(ctx, 300, "b"); err != nil {
		t.Fatal(err)
	}
	if nodes, _ := ring.Node(ctx, 300); len(nodes) != 1 || nodes[0] != "b2" {
		t.Fatalf("Node(300) = %v, want [b2]", nodes)
	}

	// Removing the last node id unlinks the node itself.
	if err := ring.Rem(ctx, 300, "b2"); err != nil {
		t.Fatal(err)
	}
	if _, err := ring.Node(ctx, 300); err == nil {
		t.Fatal("Node on a removed score should fail")
	}
	if score, _ := ring.Ceiling(ctx, 300); score != 500 {
		t.Fatalf("Ceiling(300) = %d, want 500", score)
	}

	// Rem errors on missing scores / node ids.
	if err := ring.Rem(ctx, 300, "b2"); err == nil {
		t.Fatal("Rem on a missing score should fail")
	}
	if err := ring.Rem(ctx, 500, "zzz"); err == nil {
		t.Fatal("Rem of a missing node id should fail")
	}
}

func TestSkiplistHashRing_bookkeeping(t *testing.T) {
	ring := NewSkiplistHashRing()
	ctx := context.Background()

	if err := ring.AddNodeToReplica(ctx, "a", 5); err != nil {
		t.Fatal(err)
	}
	if err := ring.AddNodeToReplica(ctx, "b", 3); err != nil {
		t.Fatal(err)
	}
	nodes, err := ring.Nodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes["a"] != 5 || nodes["b"] != 3 {
		t.Fatalf("Nodes = %v", nodes)
	}

	// The returned map must be a copy: mutating it must not touch the ring.
	nodes["a"] = 999
	nodes["ghost"] = 1
	fresh, _ := ring.Nodes(ctx)
	if fresh["a"] != 5 {
		t.Fatalf("Nodes returned the internal map: %v", fresh)
	}
	if _, ok := fresh["ghost"]; ok {
		t.Fatal("Nodes returned the internal map")
	}

	if err := ring.DeleteNodeToReplica(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if nodes, _ = ring.Nodes(ctx); len(nodes) != 1 {
		t.Fatalf("Nodes after delete = %v", nodes)
	}

	if err := ring.AddNodeToDataKeys(ctx, "a", map[string]struct{}{"k1": {}, "k2": {}}); err != nil {
		t.Fatal(err)
	}
	// Merging with existing keys must keep them.
	if err := ring.AddNodeToDataKeys(ctx, "a", map[string]struct{}{"k2": {}, "k3": {}}); err != nil {
		t.Fatal(err)
	}
	keys, err := ring.DataKeys(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("DataKeys = %v, want 3 keys", keys)
	}
	// The returned map must be a copy too.
	delete(keys, "k1")
	if keys, _ = ring.DataKeys(ctx, "a"); len(keys) != 3 {
		t.Fatalf("DataKeys returned the internal map: %v", keys)
	}

	if err := ring.DeleteNodeToDataKeys(ctx, "a", map[string]struct{}{"k1": {}, "k2": {}}); err != nil {
		t.Fatal(err)
	}
	if keys, _ = ring.DataKeys(ctx, "a"); len(keys) != 1 {
		t.Fatalf("DataKeys after delete = %v, want 1 key", keys)
	}
	// Deleting the last key drops the entry entirely.
	if err := ring.DeleteNodeToDataKeys(ctx, "a", map[string]struct{}{"k3": {}}); err != nil {
		t.Fatal(err)
	}
	if keys, _ = ring.DataKeys(ctx, "a"); keys != nil {
		t.Fatalf("DataKeys = %v, want nil", keys)
	}
	// Deleting from an unknown node is a no-op, not an error.
	if err := ring.DeleteNodeToDataKeys(ctx, "ghost", map[string]struct{}{"k9": {}}); err != nil {
		t.Fatalf("DeleteNodeToDataKeys on unknown node: %v", err)
	}
}

func TestSkiplistHashRing_concurrentAccess(t *testing.T) {
	ring := NewSkiplistHashRing()
	ctx := context.Background()

	const scores = 8
	for i := 0; i < scores; i++ {
		if err := ring.Add(ctx, int32(100*(i+1)), "seed"); err != nil {
			t.Fatal(err)
		}
	}

	const workers = 8
	const iterations = 200
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("worker_%d", w)
			for i := 0; i < iterations; i++ {
				score := int32(100*(i%scores) + 50)
				dataKey := fmt.Sprintf("k%d_%d", w, i)
				// Writers and readers on shared scores; errors are ignored on
				// purpose: the goal is race/deadlock freedom, and interleaved
				// workers may legitimately remove each other's entries.
				_ = ring.Add(ctx, score, nodeID)
				_, _ = ring.Ceiling(ctx, score)
				_, _ = ring.Floor(ctx, score)
				_, _ = ring.Node(ctx, score)
				// Iterate the result like ConsistentHash does, so a ring that
				// hands out its internal map is caught by -race.
				if nodes, err := ring.Nodes(ctx); err == nil {
					for node := range nodes {
						if node == "" {
							t.Error("empty node id in Nodes")
						}
					}
				}
				if keys, err := ring.DataKeys(ctx, nodeID); err == nil {
					for dataKey := range keys {
						if dataKey == "" {
							t.Error("empty data key in DataKeys")
						}
					}
				}
				_ = ring.AddNodeToDataKeys(ctx, nodeID, map[string]struct{}{dataKey: {}})
				_ = ring.AddNodeToReplica(ctx, nodeID, i)
				_ = ring.DeleteNodeToDataKeys(ctx, nodeID, map[string]struct{}{dataKey: {}})
				_ = ring.DeleteNodeToReplica(ctx, nodeID)
				_ = ring.Rem(ctx, score, nodeID)
			}
		}(w)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("concurrent ring access deadlocked")
	}
}

func TestSkiplistHashRing_lockContention(t *testing.T) {
	ring := NewSkiplistHashRing()
	ctx := context.Background()

	const workers = 4
	const iterations = 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if err := ring.Lock(ctx, 1); err != nil {
					t.Error(err)
					return
				}
				if err := ring.Unlock(ctx); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("lock contention deadlocked")
	}
}

func TestSkiplistHashRing_lockTTLAutoRelease(t *testing.T) {
	ring := NewSkiplistHashRing()
	ctx := context.Background()

	if err := ring.Lock(ctx, 1); err != nil {
		t.Fatal(err)
	}

	acquired := make(chan error, 1)
	go func() {
		if err := ring.Lock(ctx, 1); err != nil {
			acquired <- err
			return
		}
		acquired <- nil
		// Release the second lease from its own goroutine: the token is per goroutine.
		if err := ring.Unlock(ctx); err != nil {
			acquired <- err
		}
	}()

	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("lock TTL did not auto-release")
	}

	// The first lease expired, so unlocking it again must be rejected.
	if err := ring.Unlock(ctx); err == nil {
		t.Fatal("unlock of an expired lease should fail")
	}
}
