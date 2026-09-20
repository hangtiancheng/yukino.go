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
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/wal"
)

// constructTree reads sst files and reconstructs the tree topology.
func (t *Tree) constructTree() error {
	// List sst files in the directory, sorted by level and seq.
	sstEntries, err := t.getSortedSSTEntries()
	if err != nil {
		return err
	}

	// Load each sst file as a node into the tree.
	for _, sstEntry := range sstEntries {
		if err = t.loadNode(sstEntry); err != nil {
			return err
		}
	}

	return nil
}

func (t *Tree) getSortedSSTEntries() ([]fs.DirEntry, error) {
	allEntries, err := os.ReadDir(t.conf.Dir)
	if err != nil {
		return nil, err
	}

	sstEntries := make([]fs.DirEntry, 0, len(allEntries))
	for _, entry := range allEntries {
		if entry.IsDir() {
			continue
		}

		if !isSSTFile(entry.Name()) {
			continue
		}

		sstEntries = append(sstEntries, entry)
	}

	sort.Slice(sstEntries, func(i, j int) bool {
		levelI, seqI := getLevelSeqFromSSTFile(sstEntries[i].Name())
		levelJ, seqJ := getLevelSeqFromSSTFile(sstEntries[j].Name())
		if levelI == levelJ {
			return seqI < seqJ
		}
		return levelI < levelJ
	})
	return sstEntries, nil
}

// loadNode loads one sst file as a node into the tree.
func (t *Tree) loadNode(sstEntry fs.DirEntry) error {
	// Create the sst reader.
	sstReader, err := NewSSTReader(sstEntry.Name(), t.conf)
	if err != nil {
		return err
	}

	// Read the per-block filter data.
	blockToFilter, err := sstReader.ReadFilter()
	if err != nil {
		return err
	}

	// Read the index data.
	index, err := sstReader.ReadIndex()
	if err != nil {
		return err
	}

	// Get the sst file size in bytes.
	size, err := sstReader.Size()
	if err != nil {
		return err
	}

	// Parse the level and seq from the file name.
	level, seq := getLevelSeqFromSSTFile(sstEntry.Name())
	// Insert the node into the tree.
	t.insertNodeWithReader(sstReader, level, seq, size, blockToFilter, index)
	return nil
}

// isSSTFile reports whether name matches the level_seq.sst naming scheme.
func isSSTFile(name string) bool {
	if !strings.HasSuffix(name, ".sst") {
		return false
	}

	parts := strings.Split(strings.TrimSuffix(name, ".sst"), "_")
	if len(parts) != 2 {
		return false
	}

	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func getLevelSeqFromSSTFile(file string) (level int, seq int32) {
	file = strings.ReplaceAll(file, ".sst", "")
	arr := strings.Split(file, "_")
	level, _ = strconv.Atoi(arr[0])
	if len(arr) > 1 {
		_seq, _ := strconv.Atoi(arr[1])
		seq = int32(_seq)
	}
	return level, seq
}

// constructMemtable reads wal files and reconstructs the memtable.
func (t *Tree) constructMemtable() error {
	// 1. Read the wal directory.
	raw, err := os.ReadDir(path.Join(t.conf.Dir, "walfile"))
	if err != nil {
		return err
	}

	// 2. Filter to .wal files.
	var wals []fs.DirEntry
	for _, entry := range raw {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".wal") {
			continue
		}

		wals = append(wals, entry)
	}

	// 3. If no wal files exist, create a fresh memtable.
	if len(wals) == 0 {
		return t.newMemTable()
	}

	// 4. Restore memtables. The last one becomes the active memtable;
	// earlier ones become read-only memtables sent to the compaction channel.
	return t.restoreMemTable(wals)
}

// restoreMemTable restores read-only memtables and one active memtable from wal files.
func (t *Tree) restoreMemTable(wals []fs.DirEntry) error {
	// 1. Sort wal files by index (monotonically increasing = newer data).
	sort.Slice(wals, func(i, j int) bool {
		indexI := walFileToMemTableIndex(wals[i].Name())
		indexJ := walFileToMemTableIndex(wals[j].Name())
		return indexI < indexJ
	})

	// 2. Restore each memtable.
	for i := range wals {
		name := wals[i].Name()
		file := path.Join(t.conf.Dir, "walfile", name)

		// Build a wal reader for this file.
		walReader, err := wal.NewWALReader(file)
		if err != nil {
			return err
		}
		defer walReader.Close()

		// Read the wal content into a memtable.
		memtable := t.conf.MemTableConstructor()
		if err = walReader.RestoreToMemtable(memtable); err != nil {
			return err
		}

		if i == len(wals)-1 { // Last wal: the memtable becomes the active read-write memtable.
			t.memTable = memtable
			t.memTableIndex = walFileToMemTableIndex(name)
			t.walWriter, err = wal.NewWALWriter(file)
			if err != nil {
				return err
			}
		} else { // Earlier wals: read-only memtables sent to the compaction channel.
			memTableCompactItem := memTableCompactItem{
				walFile:  file,
				memTable: memtable,
			}

			// The compaction goroutine may already be reclaiming earlier items;
			// dataLock keeps this append exclusive with it (same as the Put path).
			t.dataLock.Lock()
			t.rOnlyMemTable = append(t.rOnlyMemTable, &memTableCompactItem)
			t.dataLock.Unlock()
			t.memCompactC <- &memTableCompactItem
		}
	}
	return nil
}
