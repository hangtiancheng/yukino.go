package consistent_hash

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/consistent_hash/local"
)

func TestFnvHasher(t *testing.T) {
	hasher := NewFnvHasher()
	if hasher.Encrypt("abc") != hasher.Encrypt("abc") {
		t.Fatal("Encrypt must be deterministic")
	}
	if score := hasher.Encrypt("abc"); score < 0 || score >= math.MaxInt32 {
		t.Fatalf("Encrypt out of range: %d", score)
	}
}

// stubEncryptor maps fixed keys to fixed scores so ring arcs are known
// upfront and migration outcomes are deterministic. Unknown keys fall back
// to FNV-1a.
type stubEncryptor struct {
	scores map[string]int32
}

func newStubEncryptor(scores map[string]int32) *stubEncryptor {
	return &stubEncryptor{scores: scores}
}

func (s *stubEncryptor) Encrypt(origin string) int32 {
	if score, ok := s.scores[origin]; ok {
		return score
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(origin))
	return int32(hasher.Sum32() % math.MaxInt32)
}

// recordingMigrator collects migration calls; migration tasks run in
// goroutines, so access must be guarded.
type recordingMigrator struct {
	mu    sync.Mutex
	calls []migrationCall
}

type migrationCall struct {
	from     string
	to       string
	dataKeys map[string]struct{}
}

func (r *recordingMigrator) migrate(_ context.Context, dataKeys map[string]struct{}, from, to string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, migrationCall{from: from, to: to, dataKeys: dataKeys})
	return nil
}

func (r *recordingMigrator) callsSince(n int) []migrationCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]migrationCall(nil), r.calls[n:]...)
}

func (r *recordingMigrator) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// TestConsistentHash_lifecycle drives AddNode / GetNode / RemoveNode over a
// stub hash: node_a owns scores {100,300,500,700,900}, node_b owns
// {200,400,600,800,1000}, and the data keys are spread so that adding or
// removing node_b must move exactly one key per affected arc — including the
// wrap-around arc at score 1000.
func TestConsistentHash_lifecycle(t *testing.T) {
	scores := map[string]int32{
		// Virtual node keys: <nodeID>_<index>.
		"node_a_0": 100, "node_a_1": 300, "node_a_2": 500, "node_a_3": 700, "node_a_4": 900,
		"node_b_0": 200, "node_b_1": 400, "node_b_2": 600, "node_b_3": 800, "node_b_4": 1000,
		// Data keys and their hash positions.
		"d00": 150, "d01": 250, "d02": 350, "d03": 450, "d04": 550,
		"d05": 650, "d06": 750, "d07": 850, "d08": 50, "d09": 950,
	}
	// Only d00, d02, d04, d06, d09 fall into node_b's arcs.
	nodeBKeys := map[string]bool{"d00": true, "d02": true, "d04": true, "d06": true, "d09": true}

	ring := local.NewSkiplistHashRing()
	rec := &recordingMigrator{}
	ch := NewConsistentHash(ring, newStubEncryptor(scores), rec.migrate, WithReplicas(5))
	ctx := context.Background()

	// An empty ring cannot serve any node.
	if _, err := ch.GetNode(ctx, "d00"); err == nil {
		t.Fatal("GetNode on an empty ring should fail")
	}

	// A weight of 0 is clamped to 1, giving node_a its 5 virtual scores.
	if err := ch.AddNode(ctx, "node_a", 0); err != nil {
		t.Fatal(err)
	}

	// Duplicate nodes are rejected.
	if err := ch.AddNode(ctx, "node_a", 1); err == nil || err.Error() != "repeat node" {
		t.Fatalf("duplicate AddNode error = %v, want repeat node", err)
	}

	// With node_a alone, every data key resolves to node_a.
	for key := range scores {
		if len(key) != 3 {
			continue // skip the virtual node keys
		}
		node, err := ch.GetNode(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if node != "node_a" {
			t.Fatalf("GetNode(%s) = %q, want node_a", key, node)
		}
	}

	before := rec.total()
	if err := ch.AddNode(ctx, "node_b", 1); err != nil {
		t.Fatal(err)
	}

	// Every AddNode migration must move exactly node_b's keys from node_a.
	addCalls := rec.callsSince(before)
	migrated := map[string]bool{}
	for _, call := range addCalls {
		if call.to != "node_b" || call.from != "node_a" {
			t.Fatalf("AddNode migration %s -> %s, want node_a -> node_b", call.from, call.to)
		}
		for dataKey := range call.dataKeys {
			if migrated[dataKey] {
				t.Fatalf("key %q migrated twice on AddNode", dataKey)
			}
			migrated[dataKey] = true
		}
	}
	for dataKey := range nodeBKeys {
		if !migrated[dataKey] {
			t.Fatalf("key %q was not migrated to node_b", dataKey)
		}
	}
	if len(migrated) != len(nodeBKeys) {
		t.Fatalf("unexpected keys migrated: %v", migrated)
	}

	// Ownership after the add: each key lands on the first score >= its hash.
	for dataKey, want := range map[string]string{
		"d00": "node_b", "d01": "node_a", "d02": "node_b", "d03": "node_a",
		"d04": "node_b", "d05": "node_a", "d06": "node_b", "d07": "node_a",
		"d08": "node_a", "d09": "node_b",
	} {
		if node, err := ch.GetNode(ctx, dataKey); err != nil {
			t.Fatal(err)
		} else if node != want {
			t.Fatalf("GetNode(%s) = %q, want %q", dataKey, node, want)
		}
	}

	// Removing an unknown node is rejected.
	if err := ch.RemoveNode(ctx, "node_x"); err == nil || err.Error() != "invalid node id" {
		t.Fatalf("RemoveNode of unknown node error = %v, want invalid node id", err)
	}

	before = rec.total()
	if err := ch.RemoveNode(ctx, "node_b"); err != nil {
		t.Fatal(err)
	}

	// Every RemoveNode migration must move node_b's keys back to node_a.
	removeCalls := rec.callsSince(before)
	migrated = map[string]bool{}
	for _, call := range removeCalls {
		if call.from != "node_b" || call.to != "node_a" {
			t.Fatalf("RemoveNode migration %s -> %s, want node_b -> node_a", call.from, call.to)
		}
		for dataKey := range call.dataKeys {
			if migrated[dataKey] {
				t.Fatalf("key %q migrated twice on RemoveNode", dataKey)
			}
			migrated[dataKey] = true
		}
	}
	if len(migrated) != len(nodeBKeys) {
		t.Fatalf("RemoveNode migrated %v, want exactly node_b's keys", migrated)
	}

	// All keys are back on the only remaining node.
	for dataKey := range scores {
		if len(dataKey) != 3 {
			continue
		}
		if node, err := ch.GetNode(ctx, dataKey); err != nil {
			t.Fatal(err)
		} else if node != "node_a" {
			t.Fatalf("after RemoveNode, GetNode(%s) = %q, want node_a", dataKey, node)
		}
	}
}

func TestConsistentHash_concurrent(t *testing.T) {
	ring := local.NewSkiplistHashRing()
	// A non-nil migrator keeps the migrateIn/migrateOut paths in play.
	ch := NewConsistentHash(ring, NewFnvHasher(), func(_ context.Context, _ map[string]struct{}, _, _ string) error {
		return nil
	}, WithReplicas(5))
	ctx := context.Background()

	// A seed node keeps the ring non-empty for GetNode while workers churn.
	if err := ch.AddNode(ctx, "seed", 1); err != nil {
		t.Fatal(err)
	}

	const workers = 4
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("node_%d", w)
			if err := ch.AddNode(ctx, nodeID, 1); err != nil {
				t.Errorf("AddNode: %v", err)
				return
			}
			for i := 0; i < 50; i++ {
				if _, err := ch.GetNode(ctx, fmt.Sprintf("key_%d_%d", w, i)); err != nil {
					t.Errorf("GetNode: %v", err)
					return
				}
			}
			if err := ch.RemoveNode(ctx, nodeID); err != nil {
				t.Errorf("RemoveNode: %v", err)
				return
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
		t.Fatal("concurrent AddNode/GetNode/RemoveNode deadlocked")
	}

	// Every worker node was removed again; only the seed remains.
	nodes, err := ring.Nodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes after churn = %v, want only the seed", nodes)
	}
}
