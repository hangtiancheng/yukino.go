package lsm_tree

import (
	"bytes"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/memtable"
	"github.com/hangtiancheng/yukino.go/components/lsm_tree/wal"
)

type Tree struct {
	conf *Config

	dataLock sync.RWMutex

	levelLocks []sync.RWMutex

	memTable memtable.MemTable

	rOnlyMemTable []*memTableCompactItem

	walWriter *wal.WALWriter

	nodes [][]*Node

	memCompactC chan *memTableCompactItem

	levelCompactC chan int

	stopChan chan struct{}

	memTableIndex int

	levelToSeq []atomic.Int32

	closeOnce sync.Once

	compactDone chan struct{}

	destroyWG sync.WaitGroup
}

func NewTree(conf *Config) (*Tree, error) {
	t := Tree{
		conf:          conf,
		memCompactC:   make(chan *memTableCompactItem),
		levelCompactC: make(chan int),
		stopChan:      make(chan struct{}),
		levelToSeq:    make([]atomic.Int32, conf.MaxLevel),
		nodes:         make([][]*Node, conf.MaxLevel),
		levelLocks:    make([]sync.RWMutex, conf.MaxLevel),
	}

	if err := t.constructTree(); err != nil {
		t.Close()
		return nil, err
	}

	t.compactDone = make(chan struct{})
	go t.compact()

	if err := t.constructMemtable(); err != nil {
		t.Close()
		return nil, err
	}

	return &t, nil
}

func (t *Tree) Close() {
	t.closeOnce.Do(func() {
		close(t.stopChan)

		if t.compactDone != nil {
			<-t.compactDone
		}

		t.destroyWG.Wait()

		t.dataLock.Lock()
		if t.walWriter != nil {
			t.walWriter.Close()
		}
		t.dataLock.Unlock()

		for i := 0; i < len(t.nodes); i++ {
			t.levelLocks[i].Lock()
			for j := 0; j < len(t.nodes[i]); j++ {
				t.nodes[i][j].Close()
			}
			t.levelLocks[i].Unlock()
		}
	})
}

func (t *Tree) Put(key, value []byte) error {
	t.dataLock.Lock()
	defer t.dataLock.Unlock()

	if err := t.walWriter.Write(key, value); err != nil {
		return err
	}

	t.memTable.Put(key, value)

	if uint64(t.memTable.Size()*5/4) <= t.conf.SSTSize {
		return nil
	}

	return t.refreshMemTableLocked()
}

func (t *Tree) Get(key []byte) ([]byte, bool, error) {
	t.dataLock.RLock()
	value, ok := t.memTable.Get(key)
	if ok {
		t.dataLock.RUnlock()
		return value, true, nil
	}

	for _, v := range slices.Backward(t.rOnlyMemTable) {
		value, ok = v.memTable.Get(key)
		if ok {
			t.dataLock.RUnlock()
			return value, true, nil
		}
	}
	t.dataLock.RUnlock()

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

	return nil, false, nil
}

func (t *Tree) refreshMemTableLocked() error {
	oldItem := memTableCompactItem{
		walFile:  t.walFile(),
		memTable: t.memTable,
	}
	t.rOnlyMemTable = append(t.rOnlyMemTable, &oldItem)
	t.walWriter.Close()
	go func() {
		select {
		case t.memCompactC <- &oldItem:
		case <-t.stopChan:
		}
	}()

	t.memTableIndex++
	return t.newMemTable()
}

func (t *Tree) levelBinarySearch(level int, key []byte, start, end int) (*Node, bool) {
	if start > end {
		return nil, false
	}

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
