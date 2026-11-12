package lsm_tree

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/wal"
)

func (t *Tree) constructTree() error {
	sstEntries, err := t.getSortedSSTEntries()
	if err != nil {
		return err
	}

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

func (t *Tree) loadNode(sstEntry fs.DirEntry) error {
	level, seq := getLevelSeqFromSSTFile(sstEntry.Name())
	if level < 0 || level >= len(t.nodes) || seq < 0 {
		return fmt.Errorf("invalid sst filename: %s", sstEntry.Name())
	}
	sstReader, err := NewSSTReader(sstEntry.Name(), t.conf)
	if err != nil {
		return err
	}
	keepReader := false
	defer func() {
		if !keepReader {
			sstReader.Close()
		}
	}()

	blockToFilter, err := sstReader.ReadFilter()
	if err != nil {
		return err
	}

	index, err := sstReader.ReadIndex()
	if err != nil {
		return err
	}
	if len(index) == 0 {
		return fmt.Errorf("sst %s has no index", sstEntry.Name())
	}

	size, err := sstReader.Size()
	if err != nil {
		return err
	}

	t.insertNodeWithReader(sstReader, level, seq, size, blockToFilter, index)
	keepReader = true
	return nil
}

func isSSTFile(name string) bool {
	if !strings.HasSuffix(name, ".sst") {
		return false
	}

	parts := strings.Split(strings.TrimSuffix(name, ".sst"), "_")
	if len(parts) != 2 {
		return false
	}

	level, err := strconv.Atoi(parts[0])
	if err != nil || level < 0 {
		return false
	}
	seq, err := strconv.ParseInt(parts[1], 10, 32)
	return err == nil && seq >= 0
}

func getLevelSeqFromSSTFile(file string) (level int, seq int32) {
	file = strings.ReplaceAll(file, ".sst", "")
	arr := strings.Split(file, "_")
	level, _ = strconv.Atoi(arr[0])
	if len(arr) > 1 {
		parsed, _ := strconv.ParseInt(arr[1], 10, 32)
		seq = int32(parsed)
	}
	return level, seq
}

func (t *Tree) constructMemtable() error {
	raw, err := os.ReadDir(path.Join(t.conf.Dir, "walfile"))
	if err != nil {
		return err
	}

	var wals []fs.DirEntry
	for _, entry := range raw {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".wal") {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".wal")); err != nil {
			continue
		}

		wals = append(wals, entry)
	}

	if len(wals) == 0 {
		return t.newMemTable()
	}

	return t.restoreMemTable(wals)
}

func (t *Tree) restoreMemTable(wals []fs.DirEntry) error {
	sort.Slice(wals, func(i, j int) bool {
		indexI := walFileToMemTableIndex(wals[i].Name())
		indexJ := walFileToMemTableIndex(wals[j].Name())
		return indexI < indexJ
	})

	for i := range wals {
		name := wals[i].Name()
		file := path.Join(t.conf.Dir, "walfile", name)

		walReader, err := wal.NewWALReader(file)
		if err != nil {
			return err
		}
		defer walReader.Close()

		memtable := t.conf.MemTableConstructor()
		if err = walReader.RestoreToMemtable(memtable); err != nil {
			return err
		}

		if i == len(wals)-1 {
			t.memTable = memtable
			t.memTableIndex = walFileToMemTableIndex(name)
			t.walWriter, err = wal.NewWALWriter(file)
			if err != nil {
				return err
			}
		} else {
			memTableCompactItem := memTableCompactItem{
				walFile:  file,
				memTable: memtable,
			}

			t.dataLock.Lock()
			t.rOnlyMemTable = append(t.rOnlyMemTable, &memTableCompactItem)
			t.dataLock.Unlock()
			t.memCompactC <- &memTableCompactItem
		}
	}
	return nil
}
