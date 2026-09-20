package raft

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"
)

type testCluster struct {
	peers map[uint64]*testNode
	done  chan struct{}
}

type testNode struct {
	id   uint64
	node Node
	c    *testCluster

	mu         sync.Mutex
	state      StateType
	applied    []string
	confs      []ConfState
	readStates []ReadState
}

func newTestCluster(ids ...uint64) *testCluster {
	c := &testCluster{
		peers: make(map[uint64]*testNode, len(ids)),
		done:  make(chan struct{}),
	}

	peers := make([]Peer, 0, len(ids))
	for _, id := range ids {
		peers = append(peers, Peer{ID: id})
	}

	for _, id := range ids {
		tn := &testNode{
			id: id,
			node: StartNode(&Config{
				ID:            id,
				ElectionTick:  10,
				HeartbeatTick: 1,
				Storage:       NewMemoryStorage(),
			}, peers),
		}
		tn.c = c
		c.peers[id] = tn

		go tn.serve()
		go tn.tickLoop()
	}

	return c
}

func (c *testCluster) stop() {
	close(c.done)
}

func (tn *testNode) tickLoop() {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			tn.node.Tick()
		case <-tn.c.done:
			return
		}
	}
}

// serve consumes ready states, forwards messages to peers, applies committed
// entries and advances.
func (tn *testNode) serve() {
	for rd := range tn.node.Ready() {
		tn.observe(rd)
		tn.dispatch(rd.Message)
		tn.applyCommitted(rd.CommittedEntries)
		tn.node.Advance()
	}
}

func (tn *testNode) observe(rd Ready) {
	tn.mu.Lock()
	defer tn.mu.Unlock()

	if rd.SoftState != nil {
		tn.state = rd.SoftState.RaftState
	}
	tn.readStates = append(tn.readStates, rd.ReadStates...)
}

func (tn *testNode) dispatch(msgs []Message) {
	for _, m := range msgs {
		dst, ok := tn.c.peers[m.To]
		if !ok {
			continue
		}
		go func(m Message, dst *testNode) {
			dst.node.recvChan <- m
		}(m, dst)
	}
}

func (tn *testNode) applyCommitted(entries []Entry) {
	for _, ent := range entries {
		if ent.Type == EntryConfChange {
			var cc ConfChange
			_ = json.Unmarshal(ent.Data, &cc)
			cs := tn.node.ApplyConfChange(cc)

			tn.mu.Lock()
			tn.confs = append(tn.confs, cs)
			tn.mu.Unlock()
			continue
		}

		if len(ent.Data) == 0 {
			continue
		}

		tn.mu.Lock()
		tn.applied = append(tn.applied, string(ent.Data))
		tn.mu.Unlock()
	}
}

func (tn *testNode) snapshot() (state StateType, applied []string, confs int, readStates []ReadState) {
	tn.mu.Lock()
	defer tn.mu.Unlock()

	return tn.state, slices.Clone(tn.applied), len(tn.confs), slices.Clone(tn.readStates)
}

func (tn *testNode) isLeader() bool {
	state, _, _, _ := tn.snapshot()
	return state == StateLeader
}

func waitCond(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

func (c *testCluster) waitLeader(t *testing.T) *testNode {
	t.Helper()

	waitCond(t, 5*time.Second, func() bool {
		for _, tn := range c.peers {
			if tn.isLeader() {
				return true
			}
		}
		return false
	}, "no leader elected in time")

	for _, tn := range c.peers {
		if tn.isLeader() {
			return tn
		}
	}
	t.Fatal("unreachable")
	return nil
}

func (c *testCluster) waitApplied(t *testing.T, want []string) {
	t.Helper()

	waitCond(t, 5*time.Second, func() bool {
		for _, tn := range c.peers {
			_, applied, _, _ := tn.snapshot()
			if !slices.Equal(applied, want) {
				return false
			}
		}
		return true
	}, "cluster did not converge on the expected applied entries")
}

func TestSingleNodeFlow(t *testing.T) {
	c := newTestCluster(1)
	defer c.stop()

	leader := c.waitLeader(t)

	if err := leader.node.Propose(context.Background(), []byte("a")); err != nil {
		t.Fatal(err)
	}
	c.waitApplied(t, []string{"a"})

	// A single-member cluster confirms reads immediately.
	if err := leader.node.ReadIndex(context.Background(), []byte("read-1")); err != nil {
		t.Fatal(err)
	}
	waitCond(t, 5*time.Second, func() bool {
		_, _, _, readStates := leader.snapshot()
		return len(readStates) > 0 && string(readStates[0].RequestCtx) == "read-1"
	}, "single-node read index not confirmed in time")

	_, _, _, readStates := leader.snapshot()
	if readStates[0].Index != 2 {
		t.Fatalf("expected read index 2, got %d", readStates[0].Index)
	}
}

func TestThreeNodeCluster(t *testing.T) {
	c := newTestCluster(1, 2, 3)
	defer c.stop()

	leader := c.waitLeader(t)

	for _, v := range []string{"v1", "v2", "v3"} {
		if err := leader.node.Propose(context.Background(), []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	c.waitApplied(t, []string{"v1", "v2", "v3"})

	// Propose two conf changes back to back: the second must be demoted to an
	// empty normal entry until the first one is applied.
	cc1 := ConfChange{ID: 1, Type: ConfChangeAddNode, NodeID: 2}
	cc2 := ConfChange{ID: 2, Type: ConfChangeAddNode, NodeID: 3}
	if err := leader.node.ProposeConfChange(context.Background(), cc1); err != nil {
		t.Fatal(err)
	}
	if err := leader.node.ProposeConfChange(context.Background(), cc2); err != nil {
		t.Fatal(err)
	}

	waitCond(t, 5*time.Second, func() bool {
		for _, tn := range c.peers {
			_, _, confs, _ := tn.snapshot()
			if confs != 1 {
				return false
			}
		}
		return true
	}, "conf changes not applied exactly once on every node")

	for _, tn := range c.peers {
		tn.mu.Lock()
		nodes := tn.confs[0].Nodes
		tn.mu.Unlock()
		if !slices.Equal(nodes, []uint64{1, 2, 3}) {
			t.Fatalf("node %d: unexpected membership %v", tn.id, nodes)
		}
	}

	// Linearizable read confirmed by a quorum of heartbeats.
	if err := leader.node.ReadIndex(context.Background(), []byte("read-1")); err != nil {
		t.Fatal(err)
	}
	waitCond(t, 5*time.Second, func() bool {
		_, _, _, readStates := leader.snapshot()
		return len(readStates) > 0 && string(readStates[0].RequestCtx) == "read-1"
	}, "read index not confirmed by quorum in time")

	_, _, _, readStates := leader.snapshot()
	// Log: no-op(1), v1-v3(2-4), cc1(5), demoted cc2(6). The demoted conf
	// change still occupies an index.
	if readStates[0].Index != 6 {
		t.Fatalf("expected read index 6, got %d", readStates[0].Index)
	}
}
