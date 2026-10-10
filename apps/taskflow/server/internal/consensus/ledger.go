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

type Ledger struct {
	node    raft.Node
	storage *raft.MemoryStorage
	id      uint64

	mu     sync.RWMutex
	state  map[string]string
	recent []AppliedEntry
	soft   *raft.SoftState
	hard   raft.HardState

	applied  atomic.Uint64
	proposed atomic.Uint64

	stopOnce sync.Once
	stopChan chan struct{}
	wg       sync.WaitGroup
}

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

func (l *Ledger) Stop() {
	if l == nil {
		return
	}
	l.stopOnce.Do(func() { close(l.stopChan) })
	l.node.Stop()
	l.wg.Wait()
}
