// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package raft

import (
	"errors"
	"sync"
)

var ErrCompacted = errors.New("requested index is unavailable due to compaction")

var ErrUnavailable = errors.New("request entry at index is unavailable")

type Storage interface {
	InitialState() (HardState, ConfState, error)
	// Entries returns log entries in the range [l, r)
	Entries(l, r uint64) ([]Entry, error)
	// Term returns the term of the entry at the given index
	Term(i uint64) (uint64, error)
	// LastIndex returns the index of the last persisted entry
	LastIndex() (uint64, error)
	// FirstIndex returns the index of the first persisted entry
	FirstIndex() (uint64, error)
	// Append persists the given entries, truncating any conflicting suffix
	Append(entries []Entry) error
	// SetHardState persists the given hard state
	SetHardState(hs HardState) error
}

type MemoryStorage struct {
	sync.Mutex
	hardState HardState
	// Persisted log entries
	entries []Entry
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

// Append persists entries to storage, truncating any conflicting suffix.
func (m *MemoryStorage) Append(entries []Entry) error {
	m.Lock()
	defer m.Unlock()

	if len(entries) == 0 {
		return nil
	}

	first := entries[0].Index
	offset := m.entries[0].Index
	if first < offset {
		// The batch is older than what is already stored; ignore it.
		return nil
	}
	if first > m.lastIndex()+1 {
		return ErrUnavailable
	}

	m.entries = append(m.entries[:first-offset], entries...)
	return nil
}

// SetHardState persists the given hard state.
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
