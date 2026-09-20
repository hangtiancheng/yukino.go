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

type unstable struct {
	// Unpersisted log entries
	entries []Entry
	// Index of the first unpersisted entry
	offset uint64
}

func (u *unstable) mustCheckOutOfBounds(l, r uint64) {
	if l > r {
		panic("invalid unstable slice")
	}

	if l < u.offset || r > u.offset+uint64(len(u.entries)) {
		panic("invalid unstable slice")
	}
}

func (u *unstable) slice(l, r uint64) []Entry {
	u.mustCheckOutOfBounds(l, r)
	return u.entries[l-u.offset : r-u.offset]
}

// Appending entries to the unstable region may truncate or overlap existing data
func (u *unstable) truncateAndAppend(entries []Entry) {
	after := entries[0].Index
	switch {
	case after == u.offset+uint64(len(u.entries)):
		u.entries = append(u.entries, entries...)
	case after <= u.offset:
		u.offset = after
		u.entries = entries
	default:
		u.entries = append([]Entry{}, u.slice(u.offset, after)...)
		u.entries = append(u.entries, entries...)
	}
}

func (u *unstable) maybeLastIndex() (uint64, bool) {
	if l := len(u.entries); l != 0 {
		return u.offset + uint64(l) - 1, true
	}
	return 0, false
}

func (u *unstable) maybeTerm(i uint64) (uint64, bool) {
	if i < u.offset {
		return 0, false
	}

	last, ok := u.maybeLastIndex()
	if !ok {
		return 0, false
	}

	if i > last {
		return 0, false
	}

	return u.entries[i-u.offset].Term, true
}

func (u *unstable) stableTo(i, t uint64) {
	gt, ok := u.maybeTerm(i)
	if !ok {
		return
	}

	if t == gt && i >= u.offset {
		u.entries = u.entries[i-u.offset+1:]
		u.offset = i + 1
	}
}

type raftLog struct {
	// Storage interface providing access to persisted log entries
	storage Storage
	// Unpersisted log entries
	unstable unstable
	// Committed log index
	commitIndex uint64
	applyIndex  uint64
}

func newRaftLog(storage Storage) *raftLog {
	if storage == nil {
		panic("storage must not be nil")
	}

	r := raftLog{
		storage: storage,
	}

	firstIndex, err := storage.FirstIndex()
	if err != nil {
		panic(err)
	}

	lastIndex, err := storage.LastIndex()
	if err != nil {
		panic(err)
	}

	r.unstable.offset = lastIndex + 1
	r.commitIndex = firstIndex - 1
	r.applyIndex = firstIndex - 1
	return &r
}

func (r *raftLog) stableTo(i, t uint64) {
	r.unstable.stableTo(i, t)
}

func (r *raftLog) unstableEntries() []Entry {
	return r.unstable.entries
}

// Returns committed but not yet applied entries
func (r *raftLog) nextEntries() []Entry {
	off := max(r.applyIndex+1, r.firstIndex())
	if r.commitIndex+1 > off {
		entries, err := r.slice(off, r.commitIndex+1)
		if err != nil {
			panic(err)
		}
		return entries
	}
	return nil
}

func (r *raftLog) firstIndex() uint64 {
	index, err := r.storage.FirstIndex()
	if err != nil {
		panic(err)
	}
	return index
}

func (r *raftLog) mustCheckOutOfBounds(lo, hi uint64) error {
	if lo > hi {
		panic("invalid raft log index")
	}

	fi := r.firstIndex()
	if lo < fi {
		panic("invalid raft log index")
	}

	if hi > r.lastIndex()+1 {
		panic("invalid raft log index")
	}

	return nil
}

func (r *raftLog) slice(lo, hi uint64) ([]Entry, error) {
	r.mustCheckOutOfBounds(lo, hi)
	if lo == hi {
		return nil, nil
	}

	var entries []Entry
	if lo < r.unstable.offset {
		stable, err := r.storage.Entries(lo, min(r.unstable.offset, hi))
		if err != nil {
			panic(err)
		}

		entries = append(entries, stable...)
	}

	if hi > r.unstable.offset {
		unstable := r.unstable.slice(max(lo, r.unstable.offset), hi)
		entries = append(entries, unstable...)
	}

	return entries, nil
}

// Append only adds entries to the unstable (unpersisted) log
func (r *raftLog) append(entries ...Entry) uint64 {
	if len(entries) == 0 {
		return r.lastIndex()
	}

	if after := entries[0].Index - 1; after < r.commitIndex {
		panic("entry index less then commit index")
	}

	r.unstable.truncateAndAppend(entries)
	return r.lastIndex()
}

func (r *raftLog) lastIndex() uint64 {
	if i, ok := r.unstable.maybeLastIndex(); ok {
		return i
	}

	i, err := r.storage.LastIndex()
	if err != nil {
		panic(err)
	}

	return i
}

func (r *raftLog) lastTerm() uint64 {
	t, err := r.term(r.lastIndex())
	if err != nil {
		panic(err)
	}
	return t
}

// Checks whether the given log is at least as up-to-date as the local log
func (r *raftLog) isUpToDate(index, term uint64) bool {
	return term > r.lastTerm() || (term == r.lastTerm() && index >= r.lastIndex())
}

func (r *raftLog) appliedTo(i uint64) {
	if i == 0 {
		return
	}
	if r.commitIndex < i || i < r.applyIndex {
		panic("invalid apply index")
	}
	r.applyIndex = i
}

// Returns log entries starting from index i
func (r *raftLog) entries(i uint64) ([]Entry, error) {
	if i > r.lastIndex() {
		return nil, nil
	}
	return r.slice(i, r.lastIndex()+1)
}

func (r *raftLog) maybeCommit(maxIndex, term uint64) bool {
	if maxIndex > r.commitIndex && r.zeroTermOnErrCompacted(r.term(maxIndex)) == term {
		r.commitTo(maxIndex)
		return true
	}
	return false
}

func (r *raftLog) term(i uint64) (uint64, error) {
	dummyIndex := r.firstIndex() - 1
	if i < dummyIndex || i > r.lastIndex() {
		return 0, nil
	}

	// Retrieve from unstable storage
	if t, ok := r.unstable.maybeTerm(i); ok && t != 0 {
		return t, nil
	}

	// Retrieve from stable storage
	t, err := r.storage.Term(i)
	if err == nil {
		return t, nil
	}

	if err == ErrCompacted || err == ErrUnavailable {
		return 0, err
	}

	panic(err)
}

// Advances the commit index
func (r *raftLog) commitTo(toCommit uint64) {
	if r.commitIndex >= toCommit {
		return
	}

	if r.lastIndex() < toCommit {
		panic("commit index over last index")
	}

	r.commitIndex = toCommit
}

func (r *raftLog) zeroTermOnErrCompacted(t uint64, err error) uint64 {
	if err == nil {
		return t
	}
	if err == ErrCompacted {
		return 0
	}

	panic(err)
}

func (r *raftLog) maybeAppend(logIndex, logTerm, commitIndex uint64, entries ...Entry) (uint64, bool) {
	// Reject if the preceding entry's index and term do not match
	if !r.matchTerm(logIndex, logTerm) {
		return 0, false
	}

	lastNewI := logIndex + uint64(len(entries))
	// Find the first conflicting entry and append from that point
	conflictStart := r.findConflict(entries)
	switch {
	case conflictStart == 0:
		// No conflict: the local log already contains all the entries
	case conflictStart <= r.commitIndex:
		panic("conflict before commit index")
	default:
		offset := logIndex + 1
		r.append(entries[conflictStart-offset:]...)
	}

	// Update the commit index
	r.commitTo(min(commitIndex, lastNewI))
	return lastNewI, true
}

func (r *raftLog) findConflict(entries []Entry) uint64 {
	for _, ent := range entries {
		if r.matchTerm(ent.Index, ent.Term) {
			continue
		}

		return ent.Index
	}
	return 0
}

func (r *raftLog) matchTerm(logIndex, logTerm uint64) bool {
	t, err := r.term(logIndex)
	if err != nil {
		return false
	}
	return t == logTerm
}
