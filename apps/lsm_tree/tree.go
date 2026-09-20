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

package lsm_tree

import (
	"bytes"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/memtable"
	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/wal"
)

// Tree is an lsm tree backed by config and on-disk sst files.
// It supports writing and reading key-value pairs.
type Tree struct {
	conf *Config

	// Lock for read/write operations.
	dataLock sync.RWMutex

	// Per-level read/write locks.
	levelLocks []sync.RWMutex

	// Active read-write memtable.
	memTable memtable.MemTable

	// Read-only memtables awaiting flush.
	rOnlyMemTable []*memTableCompactItem

	// WAL writer.
	walWriter *wal.WALWriter

	// lsm tree node topology (level -> nodes).
	nodes [][]*Node

	// Signals memtable flush when the memtable reaches the size threshold.
	memCompactC chan *memTableCompactItem

	// Signals level compaction when a level reaches the size threshold.
	levelCompactC chan int

	// Signals tree shutdown.
	stopChan chan struct{}

	// memtable index; maps 1:1 to the wal file name.
	memTableIndex int

	// Per-level sstable seq counters. sst files are named level_seq.sst.
	levelToSeq []atomic.Int32

	// closeOnce guarantees the shutdown sequence runs exactly once.
	closeOnce sync.Once

	// Closed by the compaction goroutine on exit; nil until the goroutine starts.
	// Close waits on it so in-flight flushes finish before readers are closed.
	compactDone chan struct{}

	// Tracks node-destroy goroutines spawned by compaction; Close waits for
	// them so no sst file is deleted after (or during) a reopen.
	destroyWG sync.WaitGroup
}

// NewTree constructs an lsm tree.
func NewTree(conf *Config) (*Tree, error) {
	// 1. Build the tree instance.
	t := Tree{
		conf:          conf,
		memCompactC:   make(chan *memTableCompactItem),
		levelCompactC: make(chan int),
		stopChan:      make(chan struct{}),
		levelToSeq:    make([]atomic.Int32, conf.MaxLevel),
		nodes:         make([][]*Node, conf.MaxLevel),
		levelLocks:    make([]sync.RWMutex, conf.MaxLevel),
	}

	// 2. Read sst files and reconstruct the tree.
	if err := t.constructTree(); err != nil {
		t.Close()
		return nil, err
	}

	// 3. Start the compaction goroutine.
	t.compactDone = make(chan struct{})
	go t.compact()

	// 4. Read wal files and reconstruct the memtable.
	if err := t.constructMemtable(); err != nil {
		t.Close()
		return nil, err
	}

	// 5. Return the tree.
	return &t, nil
}

func (t *Tree) Close() {
	t.closeOnce.Do(func() {
		// Signal the compaction goroutine to exit.
		close(t.stopChan)

		// Wait for in-flight flushes/compactions to finish so no sst file is
		// left half-written and no node is inserted after readers are closed.
		if t.compactDone != nil {
			<-t.compactDone
		}

		// Wait for node-destroy goroutines so no sst file is deleted after
		// (or while) the directory is reused by a new tree.
		t.destroyWG.Wait()

		// Close the active wal writer; dataLock keeps this exclusive with Put.
		t.dataLock.Lock()
		if t.walWriter != nil {
			t.walWriter.Close()
		}
		t.dataLock.Unlock()

		// Close all sst readers; level locks keep this exclusive with compaction.
		for i := 0; i < len(t.nodes); i++ {
			t.levelLocks[i].Lock()
			for j := 0; j < len(t.nodes[i]); j++ {
				t.nodes[i][j].Close()
			}
			t.levelLocks[i].Unlock()
		}
	})
}

// Put writes a key-value pair to the lsm tree. The pair goes directly into the active memtable.
func (t *Tree) Put(key, value []byte) error {
	// 1. Acquire the write lock.
	t.dataLock.Lock()
	defer t.dataLock.Unlock()

	// 2. Write to the WAL first to avoid memtable data loss on crash.
	if err := t.walWriter.Write(key, value); err != nil {
		return err
	}

	// 3. Write to the active memtable.
	t.memTable.Put(key, value)

	// 4. If the memtable has not reached the level-0 sstable threshold, return.
	// Account for sst metadata overhead by using 5/4 of the raw size.
	if uint64(t.memTable.Size()*5/4) <= t.conf.SSTSize {
		return nil
	}

	// 5. Rotate the memtable.
	return t.refreshMemTableLocked()
}

// Get reads the value for a key.
func (t *Tree) Get(key []byte) ([]byte, bool, error) {
	t.dataLock.RLock()
	// 1. Check the active memtable first.
	value, ok := t.memTable.Get(key)
	if ok {
		t.dataLock.RUnlock()
		return value, true, nil
	}

	// 2. Check read-only memtables in reverse index order (newest first).
	for _, v := range slices.Backward(t.rOnlyMemTable) {
		value, ok = v.memTable.Get(key)
		if ok {
			t.dataLock.RUnlock()
			return value, true, nil
		}
	}
	t.dataLock.RUnlock()

	// 3. Check level-0 sstables in reverse index order (newest first).
	var err error
	t.levelLocks[0].RLock()
	for _, v := range slices.Backward(t.nodes[0]) {
		if value, ok, err = v.Get(key); err != nil {
			t.levelLocks[0].RUnlock()
			return nil, false, err
		}
		if ok {
			t.levelLocks[0].RUnlock()
			return value, true, nil
		}
	}
	t.levelLocks[0].RUnlock()

	// 4. Check levels 1..N. Each level requires at most one sstable interaction because
	// these levels are sorted and non-overlapping.
	for level := 1; level < len(t.nodes); level++ {
		t.levelLocks[level].RLock()
		node, ok := t.levelBinarySearch(level, key, 0, len(t.nodes[level])-1)
		if !ok {
			t.levelLocks[level].RUnlock()
			continue
		}
		if value, ok, err = node.Get(key); err != nil {
			t.levelLocks[level].RUnlock()
			return nil, false, err
		}
		if ok {
			t.levelLocks[level].RUnlock()
			return value, true, nil
		}
		t.levelLocks[level].RUnlock()
	}

	// 5. Key not found.
	return nil, false, nil
}

// refreshMemTableLocked rotates the active memtable to read-only and builds a new active memtable.
func (t *Tree) refreshMemTableLocked() error {
	// Rotate: move the active memtable to the read-only slice and send it to the compaction goroutine.
	oldItem := memTableCompactItem{
		walFile:  t.walFile(),
		memTable: t.memTable,
	}
	t.rOnlyMemTable = append(t.rOnlyMemTable, &oldItem)
	t.walWriter.Close()
	go func() {
		select {
		case t.memCompactC <- &oldItem:
		// Tree closed before the item was handed over: give up instead of leaking.
		case <-t.stopChan:
		}
	}()

	// Build a new active memtable and its WAL.
	t.memTableIndex++
	return t.newMemTable()
}

func (t *Tree) levelBinarySearch(level int, key []byte, start, end int) (*Node, bool) {
	if start > end {
		return nil, false
	}

	// A node's range is half-open: (startKey, endKey]. startKey is the index
	// separator (one byte below the node's real first key), so the next node's
	// startKey can equal this node's endKey; a key equal to startKey belongs to
	// the node on the LEFT.
	mid := start + (end-start)>>1
	if bytes.Compare(t.nodes[level][mid].endKey, key) < 0 {
		return t.levelBinarySearch(level, key, mid+1, end)
	}

	if bytes.Compare(t.nodes[level][mid].startKey, key) >= 0 {
		return t.levelBinarySearch(level, key, start, mid-1)
	}

	return t.nodes[level][mid], true
}

func (t *Tree) newMemTable() error {
	walWriter, err := wal.NewWALWriter(t.walFile())
	if err != nil {
		return err
	}
	t.walWriter = walWriter
	t.memTable = t.conf.MemTableConstructor()
	return nil
}
