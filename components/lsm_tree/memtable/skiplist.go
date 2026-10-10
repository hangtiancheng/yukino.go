package memtable

import (
	"bytes"
	"math/rand"
	"slices"
	"time"
)

type Skiplist struct {
	head       *skipNode
	entriesCnt int
	size       int
}

type skipNode struct {
	nexts      []*skipNode
	key, value []byte
}

func NewSkiplist() MemTable {
	return &Skiplist{
		head: &skipNode{},
	}
}

func (s *Skiplist) Put(key, value []byte) {
	if node := s.getNode(key); node != nil {
		s.size += (len(value) - len(node.value))
		node.value = value
		return
	}

	s.size += (len(key) + len(value))
	s.entriesCnt++
	newNodeHeight := s.roll()

	if len(s.head.nexts) < newNodeHeight {
		dif := make([]*skipNode, newNodeHeight+1-len(s.head.nexts))
		s.head.nexts = append(s.head.nexts, dif...)
	}

	newNode := skipNode{
		nexts: make([]*skipNode, newNodeHeight),
		key:   key,
		value: value,
	}

	move := s.head
	for level := newNodeHeight - 1; level >= 0; level-- {
		for move.nexts[level] != nil && bytes.Compare(move.nexts[level].key, key) < 0 {
			move = move.nexts[level]
		}

		newNode.nexts[level] = move.nexts[level]
		move.nexts[level] = &newNode
	}
}

func (s *Skiplist) Get(key []byte) ([]byte, bool) {
	if node := s.getNode(key); node != nil {
		return node.value, true
	}

	return nil, false
}

func (s *Skiplist) All() []*KV {
	if len(s.head.nexts) == 0 {
		return nil
	}

	kvs := make([]*KV, 0, s.entriesCnt)
	for move := s.head; move.nexts[0] != nil; move = move.nexts[0] {
		kvs = append(kvs, &KV{
			Key:   move.nexts[0].key,
			Value: move.nexts[0].value,
		})
	}

	return kvs
}

func (s *Skiplist) Size() int {
	return s.size
}

func (s *Skiplist) EntriesCnt() int {
	return s.entriesCnt
}

func (s *Skiplist) getNode(key []byte) *skipNode {
	move := s.head
	for level := range slices.Backward(s.head.nexts) {
		for move.nexts[level] != nil && bytes.Compare(move.nexts[level].key, key) < 0 {
			move = move.nexts[level]
		}
		if move.nexts[level] != nil && bytes.Equal(move.nexts[level].key, key) {
			return move.nexts[level]
		}
	}

	return nil
}

func (s *Skiplist) roll() int {
	var level int
	randInst := rand.New(rand.NewSource(time.Now().Unix()))
	for randInst.Intn(2) == 1 {
		level++
	}
	return level + 1
}
