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
	"fmt"
	"math"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/memtable"
)

type memTableCompactItem struct {
	walFile  string
	memTable memtable.MemTable
}

// compact runs the compaction goroutine.
func (t *Tree) compact() {
	defer close(t.compactDone)

	for {
		select {
		// Tree shutdown signal: exit.
		case <-t.stopChan:
			return
		// Read-only memtable received: flush it to a level-0 sstable.
		case memCompactItem := <-t.memCompactC:
			t.compactMemTable(memCompactItem)
		// Level compaction trigger: run a level-sorted merge between level and level+1.
		case level := <-t.levelCompactC:
			t.compactLevel(level)
		}
	}
}

// compactLevel runs a sorted merge between level and level+1.
func (t *Tree) compactLevel(level int) {
	// A queued trigger may fire after the level was emptied by an earlier compaction.
	if len(t.nodes[level]) == 0 {
		return
	}

	// Pick the nodes to merge from level and level+1.
	pickedNodes := t.pickCompactNodes(level)

	// Create the sstWriter for the level+1 output.
	seq := t.levelToSeq[level+1].Load() + 1
	sstWriter, err := NewSSTWriter(t.sstFile(level+1, seq), t.conf)
	if err != nil {
		return
	}
	defer sstWriter.Close()

	// Size limit for each level+1 sst file.
	sstLimit := t.conf.SSTSize * uint64(math.Pow10(level+1))
	// Gather all key-value pairs from the picked nodes.
	pickedKVs := t.pickedNodesToKVs(pickedNodes)
	// Iterate over the merged key-value pairs.
	for i := range pickedKVs {
		// If the new level+1 sst file exceeds the limit, flush and start a new one.
		if sstWriter.Size() > sstLimit {
			size, blockToFilter, index := sstWriter.Finish()
			t.insertNode(level+1, seq, size, blockToFilter, index)
			seq = t.levelToSeq[level+1].Load() + 1
			sstWriter, err = NewSSTWriter(t.sstFile(level+1, seq), t.conf)
			if err != nil {
				return
			}
			defer sstWriter.Close()
		}

		// Append the pair to the sstWriter.
		sstWriter.Append(pickedKVs[i].Key, pickedKVs[i].Value)
		// Flush the last sstWriter on the final pair.
		if i == len(pickedKVs)-1 {
			size, blockToFilter, index := sstWriter.Finish()
			t.insertNode(level+1, seq, size, blockToFilter, index)
		}
	}

	// Remove the merged nodes.
	t.removeNodes(level, pickedNodes)

	// Try to trigger compaction on the next level.
	t.tryTriggerCompact(level + 1)
}

// pickCompactNodes selects nodes from level and level+1 that overlap the merge range.
func (t *Tree) pickCompactNodes(level int) []*Node {
	// Merge the first half of the current level.
	startKey := t.nodes[level][0].Start()
	endKey := t.nodes[level][0].End()

	mid := len(t.nodes[level]) >> 1
	if bytes.Compare(t.nodes[level][mid].Start(), startKey) < 0 {
		startKey = t.nodes[level][mid].Start()
	}

	if bytes.Compare(t.nodes[level][mid].End(), endKey) > 0 {
		endKey = t.nodes[level][mid].End()
	}

	var pickedNodes []*Node
	// Collect overlapping nodes from level+1 and level.
	for i := level + 1; i >= level; i-- {
		for j := 0; j < len(t.nodes[i]); j++ {
			if bytes.Compare(endKey, t.nodes[i][j].Start()) < 0 || bytes.Compare(startKey, t.nodes[i][j].End()) > 0 {
				continue
			}

			pickedNodes = append(pickedNodes, t.nodes[i][j])
		}
	}

	return pickedNodes
}

// pickedNodesToKVs gathers all key-value pairs from the picked nodes, keeping the latest value per key.
func (t *Tree) pickedNodesToKVs(pickedNodes []*Node) []*KV {
	// Lower index = older data; higher index = newer data.
	// Newer data overwrites older data.
	memtable := t.conf.MemTableConstructor()
	for _, node := range pickedNodes {
		kvs, _ := node.GetAll()
		for _, kv := range kvs {
			memtable.Put(kv.Key, kv.Value)
		}
	}

	// Use the memtable for sorted iteration.
	_kvs := memtable.All()
	kvs := make([]*KV, 0, len(_kvs))
	for _, kv := range _kvs {
		kvs = append(kvs, &KV{
			Key:   kv.Key,
			Value: kv.Value,
		})
	}

	return kvs
}

// removeNodes removes the compacted nodes from the tree topology and destroys them.
func (t *Tree) removeNodes(level int, nodes []*Node) {
	// Remove old nodes from the tree's nodes slice.
outer:
	for k := range nodes {
		node := nodes[k]
		for i := level + 1; i >= level; i-- {
			for j := 0; j < len(t.nodes[i]); j++ {
				if node != t.nodes[i][j] {
					continue
				}

				t.levelLocks[i].Lock()
				t.nodes[i] = append(t.nodes[i][:j], t.nodes[i][j+1:]...)
				t.levelLocks[i].Unlock()
				continue outer
			}
		}
	}

	t.destroyWG.Add(1)
	go func() {
		defer t.destroyWG.Done()
		// Destroy old nodes: close the sst reader and delete the sst file.
		for _, node := range nodes {
			node.Destroy()
		}
	}()
}

// compactMemTable flushes a read-only memtable to a level-0 sstable.
func (t *Tree) compactMemTable(memCompactItem *memTableCompactItem) {
	// 1. Flush the memtable to a level-0 sstable.
	if err := t.flushMemTable(memCompactItem.memTable); err != nil {
		// Keep the item in rOnlyMemTable so its data stays readable and the wal is preserved.
		return
	}

	// 2. Reclaim the memtable from the read-only slice.
	t.dataLock.Lock()
	for i := 0; i < len(t.rOnlyMemTable); i++ {
		if t.rOnlyMemTable[i].memTable != memCompactItem.memTable {
			continue
		}
		t.rOnlyMemTable = t.rOnlyMemTable[i+1:]
		break
	}
	t.dataLock.Unlock()

	// 3. Delete the WAL file; the data is safely persisted in the sstable.
	_ = os.Remove(memCompactItem.walFile)
}

// flushMemTable writes the memtable to a new level-0 sst file.
func (t *Tree) flushMemTable(memTable memtable.MemTable) error {
	seq := t.levelToSeq[0].Load() + 1

	// Create the sst writer.
	sstWriter, err := NewSSTWriter(t.sstFile(0, seq), t.conf)
	if err != nil {
		return err
	}
	defer sstWriter.Close()

	// Write all memtable entries to the sst writer.
	for _, kv := range memTable.All() {
		sstWriter.Append(kv.Key, kv.Value)
	}

	// Flush the sstable.
	size, blockToFilter, index := sstWriter.Finish()

	// Insert the node into the tree.
	t.insertNode(0, seq, size, blockToFilter, index)
	// Try to trigger compaction.
	t.tryTriggerCompact(0)
	return nil
}

func (t *Tree) tryTriggerCompact(level int) {
	// No compaction on the last level.
	if level == len(t.nodes)-1 {
		return
	}

	var size uint64
	for _, node := range t.nodes[level] {
		size += node.size
	}

	if size <= t.conf.SSTSize*uint64(math.Pow10(level))*uint64(t.conf.SSTNumPerLevel) {
		return
	}

	go func() {
		select {
		case t.levelCompactC <- level:
		// Tree closed before the trigger was handed over: give up instead of leaking.
		case <-t.stopChan:
		}
	}()
}

// insertNodeWithReader inserts a node into the given level using an existing sstReader.
func (t *Tree) insertNodeWithReader(sstReader *SSTReader, level int, seq int32, size uint64, blockToFilter map[uint64][]byte, index []*Index) {
	file := t.sstFile(level, seq)
	// Record the seq for this level (monotonically increasing).
	t.levelToSeq[level].Store(seq)

	// Build the lsm node.
	newNode := NewNode(t.conf, file, sstReader, level, seq, size, blockToFilter, index)
	// Level 0: append the node.
	if level == 0 {
		t.levelLocks[0].Lock()
		t.nodes[level] = append(t.nodes[level], newNode)
		t.levelLocks[0].Unlock()
		return
	}

	// Levels 1..N: insert in key order.
	for i := 0; i < len(t.nodes[level])-1; i++ {
		// Find the first node whose min key is larger than the new node's max key, and insert before it.
		if bytes.Compare(newNode.End(), t.nodes[level][i+1].Start()) < 0 {
			t.levelLocks[level].Lock()
			t.nodes[level] = append(t.nodes[level][:i+1], t.nodes[level][i:]...)
			t.nodes[level][i+1] = newNode
			t.levelLocks[level].Unlock()
			return
		}
	}

	// If no insertion point was found, the new node has the largest key: append it.
	t.levelLocks[level].Lock()
	t.nodes[level] = append(t.nodes[level], newNode)
	t.levelLocks[level].Unlock()
}

func (t *Tree) insertNode(level int, seq int32, size uint64, blockToFilter map[uint64][]byte, index []*Index) {
	file := t.sstFile(level, seq)
	sstReader, err := NewSSTReader(file, t.conf)
	if err != nil {
		return
	}

	t.insertNodeWithReader(sstReader, level, seq, size, blockToFilter, index)
}

func (t *Tree) sstFile(level int, seq int32) string {
	return fmt.Sprintf("%d_%d.sst", level, seq)
}

func (t *Tree) walFile() string {
	return path.Join(t.conf.Dir, "walfile", fmt.Sprintf("%d.wal", t.memTableIndex))
}

func walFileToMemTableIndex(walFile string) int {
	rawIndex := strings.ReplaceAll(walFile, ".wal", "")
	index, _ := strconv.Atoi(rawIndex)
	return index
}
