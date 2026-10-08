// Package consensus embeds raft's raft core (components//raft) as
// a per-node consensus commit log. Every dispatch and finish decision is
// proposed through the raft state machine (leader election, log replication,
// Ready/Advance batching); committed entries are applied to an in-memory KV
// state machine that the monitor API can inspect.
//
// Deployment note: raft is a teaching implementation whose inter-process
// transport is stubbed, so each taskflow node runs a single-member raft group
// (quorum of one, commits land on the next Ready cycle). The log still gives
// every node an ordered, linearizable-local record of the decisions it made,
// served through the standard raft Node embedding pattern (Tick + Ready +
// Advance).
package consensus

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hangtiancheng/yukino.go/components/raft/raft"
)

const recentCap = 64

// AppliedEntry is one committed KV pair exposed by the monitor API.
type AppliedEntry struct {
	Index uint64 `json:"index"`
	Term  uint64 `json:"term"`
	Key   string `json:"key"`
	Value string `json:"value"`
	Time  string `json:"time"`
}

type payload struct {
	K string `json:"k"`
	V string `json:"v"`
	T string `json:"t"`
}

// Ledger drives one embedded raft node.
type Ledger struct {
	node    raft.Node
	storage *raft.MemoryStorage
	id      uint64

	mu     sync.RWMutex
	state  map[string]string
	recent []AppliedEntry // newest first, capped
	soft   *raft.SoftState
	hard   raft.HardState

	applied  atomic.Uint64
	proposed atomic.Uint64

	stopOnce sync.Once
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewLedger starts the raft node, its tick loop (100ms) and the Ready/Advance
// driver loop. id only needs to be unique inside this node's single-member
// group; distinct ids across nodes keep logs distinguishable.
func NewLedger(id uint64) *Ledger {
	if id == 0 {
		id = 1
	}
	storage := raft.NewMemoryStorage()
	cfg := &raft.Config{
		ID:            id,
		Storage:       storage,
		PreVote:       true,
		ElectionTick:  10,
		HeartbeatTick: 1,
	}
	l := &Ledger{
		node:     raft.StartNode(cfg, []raft.Peer{{ID: id}}),
		storage:  storage,
		id:       id,
		state:    make(map[string]string),
		recent:   make([]AppliedEntry, 0, recentCap),
		stopChan: make(chan struct{}),
	}
	l.wg.Add(2)
	go func() { defer l.wg.Done(); l.tickLoop() }()
	go func() { defer l.wg.Done(); l.readyLoop() }()
	return l
}

func (l *Ledger) tickLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-l.stopChan:
			return
		case <-ticker.C:
			l.node.Tick()
		}
	}
}

// readyLoop persists hard state, applies committed entries to the KV state
// machine and advances the raft node, mirroring raft's proxy driver.
func (l *Ledger) readyLoop() {
	for {
		select {
		case <-l.stopChan:
			return
		case rd := <-l.node.Ready():
			l.persistReady(rd)
			l.node.Advance()
		}
	}
}

func (l *Ledger) persistReady(rd raft.Ready) {
	if err := l.storage.Append(rd.Entries); err != nil {
		slog.Error("persist raft entries", "err", err)
		return
	}
	if !raft.IsEmptyHardState(rd.HardState) {
		if err := l.storage.SetHardState(rd.HardState); err != nil {
			slog.Error("persist raft state", "err", err)
			return
		}
	}
	l.mu.Lock()
	if rd.SoftState != nil {
		l.soft = rd.SoftState
	}
	if !raft.IsEmptyHardState(rd.HardState) {
		l.hard = rd.HardState
	}
	l.mu.Unlock()

	for _, ent := range rd.CommittedEntries {
		switch {
		case ent.Type == raft.EntryConfChange:
			var cc raft.ConfChange
			if err := json.Unmarshal(ent.Data, &cc); err == nil {
				l.node.ApplyConfChange(cc)
			}
		case len(ent.Data) == 0:
			// Initial empty entry at index 0; nothing to apply.
		default:
			var p payload
			if err := json.Unmarshal(ent.Data, &p); err != nil {
				slog.Warn("consensus: skip malformed committed entry", "index", ent.Index, "err", err)
				continue
			}
			l.apply(ent.Index, ent.Term, p)
		}
		l.applied.Add(1)
	}
}

func (l *Ledger) apply(index, term uint64, p payload) {
	entry := AppliedEntry{Index: index, Term: term, Key: p.K, Value: p.V, Time: p.T}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.state[p.K] = p.V
	l.recent = append([]AppliedEntry{entry}, l.recent...)
	if len(l.recent) > recentCap {
		l.recent = l.recent[:recentCap]
	}
}

// Propose submits a KV pair through raft. The proposal is asynchronous: the
// entry commits once the driver loop processes the next Ready batch.
func (l *Ledger) Propose(ctx context.Context, key, value string) error {
	if l == nil {
		return nil
	}
	body, err := json.Marshal(payload{K: key, V: value, T: time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	proposeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := l.node.Propose(proposeCtx, body); err != nil {
		return err
	}
	l.proposed.Add(1)
	return nil
}

// Get reads the applied state machine. Reads reflect committed-and-applied
// entries only.
func (l *Ledger) Get(key string) (string, bool) {
	if l == nil {
		return "", false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	v, ok := l.state[key]
	return v, ok
}

func stateName(s raft.StateType) string {
	switch s {
	case raft.StateFollower:
		return "follower"
	case raft.StateCandidate:
		return "candidate"
	case raft.StateLeader:
		return "leader"
	case raft.StatePreCandidate:
		return "pre-candidate"
	default:
		return "unknown"
	}
}

// Status snapshots raft state for the monitor API.
func (l *Ledger) Status() map[string]any {
	if l == nil {
		return map[string]any{"enabled": false}
	}
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := map[string]any{
		"enabled":            true,
		"node_id":            l.id,
		"proposed_entries":   l.proposed.Load(),
		"applied_entries":    l.applied.Load(),
		"state_machine_keys": len(l.state),
		"term":               l.hard.Term,
		"commit_index":       l.hard.CommitIndex,
	}
	if l.soft != nil {
		out["raft_state"] = stateName(l.soft.RaftState)
		out["leader_id"] = l.soft.Lead
		out["is_leader"] = l.soft.Lead == l.id
	} else {
		out["raft_state"] = "starting"
	}
	recent := make([]AppliedEntry, len(l.recent))
	copy(recent, l.recent)
	out["recent_entries"] = recent
	return out
}

// Stop halts the drivers and the underlying Raft core.
func (l *Ledger) Stop() {
	if l == nil {
		return
	}
	l.stopOnce.Do(func() { close(l.stopChan) })
	l.node.Stop()
	l.wg.Wait()
}
