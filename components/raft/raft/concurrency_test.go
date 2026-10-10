package raft

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentProposals(t *testing.T) {
	c := newTestCluster(1, 2, 3)
	defer c.stop()

	leader := c.waitLeader(t)

	const (
		workers   = 6
		perWorker = 15
		total     = workers * perWorker
	)

	var (
		wg      sync.WaitGroup
		counter atomic.Uint64
		stop    = make(chan struct{})
		done    = make(chan struct{})
	)

	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = leader.node.ReadIndex(ctx, []byte(fmt.Sprintf("read-%d", i)))
			cancel()
			time.Sleep(5 * time.Millisecond)
		}
	}()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				target := c.peers[uint64(w%3)+1]
				v := fmt.Sprintf("v-%03d", counter.Add(1)-1)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := target.node.Propose(ctx, []byte(v))
				cancel()
				if err != nil {
					t.Errorf("propose %q: %v", v, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(stop)
	<-done

	want := make([]string, 0, total)
	for i := 0; i < total; i++ {
		want = append(want, fmt.Sprintf("v-%03d", i))
	}

	waitCond(t, 15*time.Second, func() bool {
		for _, tn := range c.peers {
			_, applied, _, _ := tn.snapshot()
			if len(applied) != total {
				return false
			}
		}
		return true
	}, "cluster did not apply all proposals in time")

	_, ref, _, _ := leader.snapshot()
	sorted := slices.Clone(ref)
	slices.Sort(sorted)
	if !slices.Equal(sorted, want) {
		t.Fatalf("leader applied %d entries with unexpected content", len(ref))
	}

	for id, tn := range c.peers {
		_, applied, _, _ := tn.snapshot()
		if !slices.Equal(applied, ref) {
			t.Fatalf("node %d applied sequence diverged from node %d", id, leader.id)
		}
	}

	waitCond(t, 5*time.Second, func() bool {
		_, _, _, readStates := leader.snapshot()
		return len(readStates) > 0
	}, "no ReadIndex request completed in time")
}

func TestCampaignAfterRemovingSelf(t *testing.T) {
	c := newTestCluster(1)
	defer c.stop()

	leader := c.waitLeader(t)

	if err := leader.node.ProposeConfChange(context.Background(), ConfChange{
		ID:     1,
		Type:   ConfChangeRemoveNode,
		NodeID: 1,
	}); err != nil {
		t.Fatal(err)
	}

	waitCond(t, 5*time.Second, func() bool {
		_, _, confs, _ := leader.snapshot()
		return confs == 1
	}, "conf change not applied in time")

	_ = leader.node.Campaign(context.Background())
	time.Sleep(200 * time.Millisecond)
}

func TestStartNodeWithZeroElectionTick(t *testing.T) {
	n := StartNode(&Config{
		ID:      1,
		Storage: NewMemoryStorage(),
	}, []Peer{{ID: 1}})

	for i := 0; i < 10; i++ {
		n.Tick()
		time.Sleep(time.Millisecond)
	}
}

func TestHandleHeartbeatClampsCommitIndex(t *testing.T) {
	r := newRaft(&Config{
		ID:            1,
		ElectionTick:  10,
		HeartbeatTick: 1,
		Storage:       NewMemoryStorage(),
	})
	r.becomeFollower(1, 2)

	r.handleHeartbeat(Message{From: 2, Term: 1, CommitIndex: 100})

	if r.raftLog.commitIndex != 0 {
		t.Fatalf("commit index = %d, want 0", r.raftLog.commitIndex)
	}
}

func TestMemoryStorageConcurrentAccess(t *testing.T) {
	ms := NewMemoryStorage()

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				idx := uint64(w*1000 + i + 1)
				_ = ms.Append([]Entry{{Index: idx, Term: uint64(i%5 + 1), Data: []byte("x")}})
				_, _ = ms.LastIndex()
				_, _ = ms.FirstIndex()
				_, _ = ms.Term(idx)
				_, _ = ms.Entries(1, idx+1)
				_ = ms.SetHardState(HardState{Term: uint64(i + 1)})
				_, _, _ = ms.InitialState()
			}
		}(w)
	}
	wg.Wait()
}
