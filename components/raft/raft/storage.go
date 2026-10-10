package raft

import (
	"errors"
	"sync"
)

var ErrCompacted = errors.New("requested index is unavailable due to compaction")

var ErrUnavailable = errors.New("request entry at index is unavailable")

type Storage interface {
	InitialState() (HardState, ConfState, error)
	Entries(l, r uint64) ([]Entry, error)
	Term(i uint64) (uint64, error)
	LastIndex() (uint64, error)
	FirstIndex() (uint64, error)
	Append(entries []Entry) error
	SetHardState(hs HardState) error
}

type MemoryStorage struct {
	sync.Mutex
	hardState HardState
	entries   []Entry
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		entries: make([]Entry, 1),
	}
}

func (m *MemoryStorage) InitialState() (HardState, ConfState, error) {
	m.Lock()
	defer m.Unlock()
	return m.hardState, ConfState{}, nil
}

func (m *MemoryStorage) Entries(l, r uint64) ([]Entry, error) {
	m.Lock()
	defer m.Unlock()

	offset := m.entries[0].Index
	if l <= offset {
		return nil, ErrCompacted
	}

	if r > m.lastIndex()+1 {
		return nil, ErrUnavailable
	}

	if len(m.entries) == 1 {
		return nil, ErrUnavailable
	}

	return m.entries[l-offset : r-offset], nil

}

func (m *MemoryStorage) Term(i uint64) (uint64, error) {
	m.Lock()
	defer m.Unlock()
	offset := m.entries[0].Index
	if i < offset {
		return 0, ErrCompacted
	}

	if int(i-offset) >= len(m.entries) {
		return 0, ErrUnavailable
	}

	return m.entries[i-offset].Term, nil
}

func (m *MemoryStorage) LastIndex() (uint64, error) {
	m.Lock()
	defer m.Unlock()
	return m.lastIndex(), nil
}

func (m *MemoryStorage) lastIndex() uint64 {
	return m.entries[0].Index + uint64(len(m.entries)) - 1
}

func (m *MemoryStorage) Append(entries []Entry) error {
	m.Lock()
	defer m.Unlock()

	if len(entries) == 0 {
		return nil
	}

	first := entries[0].Index
	offset := m.entries[0].Index
	if first < offset {
		return nil
	}
	if first > m.lastIndex()+1 {
		return ErrUnavailable
	}

	m.entries = append(m.entries[:first-offset], entries...)
	return nil
}

func (m *MemoryStorage) SetHardState(hs HardState) error {
	m.Lock()
	defer m.Unlock()
	m.hardState = hs
	return nil
}

func (m *MemoryStorage) FirstIndex() (uint64, error) {
	m.Lock()
	defer m.Unlock()
	return m.firstIndex(), nil
}

func (m *MemoryStorage) firstIndex() uint64 {
	return m.entries[0].Index + 1
}
