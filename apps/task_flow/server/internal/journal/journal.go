package journal

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree"
)

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

type Store struct {
	tree *lsm_tree.Tree
	node string

	mu     sync.Mutex
	closed bool
}

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
