package raft

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestNodeShutdownUnblocksEmbedding(t *testing.T) {
	n := StartNode(&Config{ID: 1, Storage: NewMemoryStorage(), ElectionTick: 10, HeartbeatTick: 1}, []Peer{{ID: 1}})
	var callers sync.WaitGroup
	for i := 0; i < 8; i++ {
		callers.Add(1)
		go func() { defer callers.Done(); n.Stop() }()
	}
	callers.Wait()
	if err := n.Propose(context.Background(), []byte("after-stop")); !errors.Is(err, ErrStopped) {
		t.Fatalf("proposal error=%v", err)
	}
	n.Advance()
	n.ApplyConfChange(ConfChange{})
}
