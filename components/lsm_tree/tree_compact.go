package lsm_tree

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/memtable"
)

type memTableCompactItem struct {
	walFile  string
	memTable memtable.MemTable
}

func (t *Tree) compact() {
	defer close(t.compactDone)

	for {
		select {
		case <-t.stopChan:
			return
		case memCompactItem := <-t.memCompactC:
			t.compactMemTable(memCompactItem)
		case level := <-t.levelCompactC:
			t.compactLevel(level)
		}
	}
}

func (t *Tree) compactLevel(level int) {
	if len(t.nodes[level]) == 0 {
		return
	}

	pickedNodes := t.pickCompactNodes(level)

	seq := t.levelToSeq[level+1].Load() + 1
	sstWriter, err := NewSSTWriter(t.sstFile(level+1, seq), t.conf)
	if err != nil {
		return
	}
	defer sstWriter.Close()

	sstLimit := t.conf.SSTSize * uint64(math.Pow10(level+1))
	pickedKVs := t.pickedNodesToKVs(pickedNodes)
	for i := range pickedKVs {
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

		sstWriter.Append(pickedKVs[i].Key, pickedKVs[i].Value)
		if i == len(pickedKVs)-1 {
			size, blockToFilter, index := sstWriter.Finish()
			t.insertNode(level+1, seq, size, blockToFilter, index)
		}
	}

	t.removeNodes(level, pickedNodes)

	t.tryTriggerCompact(level + 1)
}

func (t *Tree) pickCompactNodes(level int) []*Node {
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

func (t *Tree) pickedNodesToKVs(pickedNodes []*Node) []*KV {
	memtable := t.conf.MemTableConstructor()
	for _, node := range pickedNodes {
		kvs, _ := node.GetAll()
		for _, kv := range kvs {
			memtable.Put(kv.Key, kv.Value)
		}
	}

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

func (t *Tree) removeNodes(level int, nodes []*Node) {
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
		for _, node := range nodes {
			node.Destroy()
		}
	}()
}

func (t *Tree) compactMemTable(memCompactItem *memTableCompactItem) {
	if err := t.flushMemTable(memCompactItem.memTable); err != nil {
		return
	}

	t.dataLock.Lock()
	for i := 0; i < len(t.rOnlyMemTable); i++ {
		if t.rOnlyMemTable[i].memTable != memCompactItem.memTable {
			continue
		}
		t.rOnlyMemTable = t.rOnlyMemTable[i+1:]
		break
	}
	t.dataLock.Unlock()

	_ = os.Remove(memCompactItem.walFile)
}

func (t *Tree) flushMemTable(memTable memtable.MemTable) error {
	seq := t.levelToSeq[0].Load() + 1

	sstWriter, err := NewSSTWriter(t.sstFile(0, seq), t.conf)
	if err != nil {
		return err
	}
	defer sstWriter.Close()

	for _, kv := range memTable.All() {
		sstWriter.Append(kv.Key, kv.Value)
	}

	size, blockToFilter, index := sstWriter.Finish()

	t.insertNode(0, seq, size, blockToFilter, index)
	t.tryTriggerCompact(0)
	return nil
}

func (t *Tree) tryTriggerCompact(level int) {
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
		case <-t.stopChan:
		}
	}()
}

func (t *Tree) insertNodeWithReader(sstReader *SSTReader, level int, seq int32, size uint64, blockToFilter map[uint64][]byte, index []*Index) {
	file := t.sstFile(level, seq)
	t.levelToSeq[level].Store(seq)

	newNode := NewNode(t.conf, file, sstReader, level, seq, size, blockToFilter, index)
	if level == 0 {
		t.levelLocks[0].Lock()
		t.nodes[level] = append(t.nodes[level], newNode)
		t.levelLocks[0].Unlock()
		return
	}

	for i := 0; i < len(t.nodes[level])-1; i++ {
		if bytes.Compare(newNode.End(), t.nodes[level][i+1].Start()) < 0 {
			t.levelLocks[level].Lock()
			t.nodes[level] = append(t.nodes[level][:i+1], t.nodes[level][i:]...)
			t.nodes[level][i+1] = newNode
			t.levelLocks[level].Unlock()
			return
		}
	}

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
