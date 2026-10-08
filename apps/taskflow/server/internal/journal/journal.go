// Package journal is the node-local audit ledger for idempotency keys. It is
// backed by the lsm_tree storage engine (components//lsm_tree): every entry
// is appended to a write-ahead log before the memtable, flushed to leveled
// SSTables with bloom filters, and compacted in the background, so the
// fire-key -> execution mapping survives crashes and restarts on each node.
package journal

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree"
)

// Entry is one audit record keyed by fire key.
type Entry struct {
	FireKey      string `json:"fire_key"`
	ExecutionID  uint   `json:"execution_id"`
	TaskType     string `json:"task_type"`
	TaskName     string `json:"task_name"`
	Status       string `json:"status"`
	Node         string `json:"node"`
	DispatchedAt string `json:"dispatched_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	Error        string `json:"error,omitempty"`
	UpdatedAt    string `json:"updated_at"`
}

// Store wraps an lsm_tree instance. The zero value is not usable; Open
// returns a ready store. A nil *Store is a valid no-op store so callers can
// run with journaling disabled.
type Store struct {
	tree *lsm_tree.Tree
	node string

	// mu serializes read-modify-write cycles on one fire key; lsm_tree
	// itself is safe for concurrent Put/Get.
	mu     sync.Mutex
	closed bool
}

// Open creates (or restores) the LSM journal rooted at dir. NewConfig
// materializes the directory tree and NewTree replays the WAL of a previous
// run.
func Open(dir, nodeID string) (*Store, error) {
	if dir == "" {
		return nil, nil
	}
	conf, err := lsm_tree.NewConfig(dir,
		lsm_tree.WithMaxLevel(4),
		lsm_tree.WithSSTSize(1<<20),
		lsm_tree.WithSSTNumPerLevel(4),
		lsm_tree.WithSSTDataBlockSize(16<<10),
	)
	if err != nil {
		return nil, fmt.Errorf("journal config: %w", err)
	}
	tree, err := lsm_tree.NewTree(conf)
	if err != nil {
		return nil, fmt.Errorf("journal tree: %w", err)
	}
	return &Store{tree: tree, node: nodeID}, nil
}

func entryKey(fireKey string) []byte { return []byte("fk:" + fireKey) }

// RecordDispatch upserts the entry for a freshly dispatched fire key. Errors
// are returned but never fail the dispatch itself; callers log them.
func (s *Store) RecordDispatch(fireKey string, executionID uint, taskType, taskName string) error {
	if s == nil || s.tree == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	entry := Entry{
		FireKey:      fireKey,
		ExecutionID:  executionID,
		TaskType:     taskType,
		TaskName:     taskName,
		Status:       "dispatched",
		Node:         s.node,
		DispatchedAt: now,
		UpdatedAt:    now,
	}
	return s.put(entry)
}

// RecordOutcome folds a terminal status into the entry for fireKey. Missing
// entries (node restarted before dispatch, or dispatch happened elsewhere)
// are created on the fly so the ledger stays queryable.
func (s *Store) RecordOutcome(fireKey string, executionID uint, status, runErr string) error {
	if s == nil || s.tree == nil || fireKey == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	entry, found, err := s.get(fireKey)
	if err != nil {
		slog.Warn("journal read before outcome failed", "fire_key", fireKey, "err", err)
	}
	if !found {
		entry = Entry{FireKey: fireKey, ExecutionID: executionID, Node: s.node}
	}
	entry.ExecutionID = executionID
	entry.Status = status
	entry.FinishedAt = now
	entry.UpdatedAt = now
	if runErr != "" {
		entry.Error = runErr
	}
	return s.put(entry)
}

// Lookup returns the stored entry for a fire key.
func (s *Store) Lookup(fireKey string) (*Entry, bool, error) {
	if s == nil || s.tree == nil {
		return nil, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, false, nil
	}
	entry, found, err := s.get(fireKey)
	if err != nil || !found {
		return nil, false, err
	}
	return &entry, true, nil
}

func (s *Store) get(fireKey string) (Entry, bool, error) {
	var entry Entry
	raw, found, err := s.tree.Get(entryKey(fireKey))
	if err != nil || !found {
		return entry, false, err
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return entry, false, fmt.Errorf("journal entry %q: %w", fireKey, err)
	}
	return entry, true, nil
}

func (s *Store) put(entry Entry) error {
	body, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.tree.Put(entryKey(entry.FireKey), body)
}

// Close flushes and closes the underlying LSM tree.
func (s *Store) Close() {
	if s == nil || s.tree == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.tree.Close()
}
