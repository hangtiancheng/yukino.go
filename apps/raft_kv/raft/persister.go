package raft

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const pairMagic = "\x00yukino-kv-state-v1\x00"

// Persister keeps Raft state and its corresponding snapshot in one atomic
// record. Older installations may have two separate files, read on startup.
type Persister struct {
	mu            sync.Mutex
	raftStatePath string
	raftState     []byte
	snapshot      []byte
}

// NewPersister opens (or creates) the persistence files for node me inside
// dir. Existing content is loaded, never discarded.
func NewPersister(dir string, me int) (*Persister, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("persister: create data dir: %w", err)
	}
	ps := &Persister{
		raftStatePath: filepath.Join(dir, fmt.Sprintf("raftstatePersist%d.txt", me)),
	}
	raftState, err := os.ReadFile(ps.raftStatePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("persister: read raft state: %w", err)
	}
	if payload, paired := bytes.CutPrefix(raftState, []byte(pairMagic)); paired {
		if len(payload) < 8 {
			return nil, fmt.Errorf("persister: truncated state header")
		}
		stateSize := binary.BigEndian.Uint64(payload[:8])
		payload = payload[8:]
		if stateSize > uint64(len(payload)) {
			return nil, fmt.Errorf("persister: truncated raft state")
		}
		ps.raftState = payload[:stateSize]
		ps.snapshot = payload[stateSize:]
		return ps, nil
	}
	snapshotPath := filepath.Join(dir, fmt.Sprintf("snapshotPersist%d.txt", me))
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("persister: read snapshot: %w", err)
	}
	ps.raftState = raftState
	ps.snapshot = snapshot
	return ps, nil
}

// SaveRaftState durably replaces the Raft state, retaining its snapshot.
func (ps *Persister) SaveRaftState(state []byte) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.saveLocked(state, ps.snapshot)
}

// Save durably replaces the Raft state and snapshot together.
func (ps *Persister) Save(raftState, snapshot []byte) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.saveLocked(raftState, snapshot)
}

func (ps *Persister) saveLocked(state, snapshot []byte) error {
	payload := make([]byte, 0, len(pairMagic)+8+len(state)+len(snapshot))
	payload = append(payload, pairMagic...)
	payload = binary.BigEndian.AppendUint64(payload, uint64(len(state)))
	payload = append(payload, state...)
	payload = append(payload, snapshot...)
	if err := writeFileAtomically(ps.raftStatePath, payload); err != nil {
		return err
	}
	ps.raftState = bytes.Clone(state)
	ps.snapshot = bytes.Clone(snapshot)
	return nil
}

// ReadRaftState returns a copy of the persisted Raft state.
func (ps *Persister) ReadRaftState() []byte {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return bytes.Clone(ps.raftState)
}

// ReadSnapshot returns a copy of the persisted snapshot.
func (ps *Persister) ReadSnapshot() []byte {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return bytes.Clone(ps.snapshot)
}

// RaftStateSize reports the byte size of the persisted Raft state. The KV
// layer uses it to decide when the log has grown large enough to warrant a
// snapshot.
func (ps *Persister) RaftStateSize() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.raftState)
}

// writeFileAtomically writes data to a temporary file, flushes it to disk
// and renames it over path. Readers therefore observe either the previous
// content or the new content, never a partial write.
func writeFileAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("persister: create temporary state file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("persister: write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("persister: sync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("persister: close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("persister: rename %s: %w", path, err)
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("persister: open directory %s: %w", dir, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("persister: sync directory %s: %w", dir, err)
	}
	return nil
}
