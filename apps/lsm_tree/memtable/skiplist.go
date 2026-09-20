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

package memtable

import (
	"bytes"
	"math/rand"
	"slices"
	"time"
)

// Skiplist is an unsynchronized skip list; it is not safe for concurrent use.
type Skiplist struct {
	head       *skipNode // head sentinel node
	entriesCnt int       // number of key-value pairs
	size       int       // data size in bytes
}

// skipNode is a node in the skip list.
type skipNode struct {
	nexts      []*skipNode // multi-level forward pointers
	key, value []byte      // stored key-value pair
}

// NewSkiplist constructs a Skiplist instance.
func NewSkiplist() MemTable {
	return &Skiplist{
		head: &skipNode{}, // initialize the head sentinel
	}
}

// Put inserts or updates a key-value pair. If the key already exists, the value is overwritten.
func (s *Skiplist) Put(key, value []byte) {
	// If the key already exists, update the value.
	if node := s.getNode(key); node != nil {
		// Adjust the size by the value-length delta.
		s.size += (len(value) - len(node.value))
		node.value = value
		return
	}

	// New key: add the key and value sizes to the total.
	s.size += (len(key) + len(value))
	s.entriesCnt++
	// Roll a height for the new node.
	newNodeHeight := s.roll()

	// Grow the head's level slice if the list is not tall enough.
	if len(s.head.nexts) < newNodeHeight {
		dif := make([]*skipNode, newNodeHeight+1-len(s.head.nexts))
		s.head.nexts = append(s.head.nexts, dif...)
	}

	// Build the new node.
	newNode := skipNode{
		nexts: make([]*skipNode, newNodeHeight),
		key:   key,
		value: value,
	}

	// Top-down level traversal, inserting the node at each level.
	move := s.head
	for level := newNodeHeight - 1; level >= 0; level-- {
		// Walk right until the next node is nil or has a larger key.
		for move.nexts[level] != nil && bytes.Compare(move.nexts[level].key, key) < 0 {
			move = move.nexts[level]
		}

		// Splice in the new node.
		newNode.nexts[level] = move.nexts[level]
		move.nexts[level] = &newNode
	}
}

// Get returns the value for the given key.
func (s *Skiplist) Get(key []byte) ([]byte, bool) {
	if node := s.getNode(key); node != nil {
		return node.value, true
	}

	return nil, false
}

// All returns all key-value pairs in ascending key order.
func (s *Skiplist) All() []*KV {
	if len(s.head.nexts) == 0 {
		return nil
	}

	kvs := make([]*KV, 0, s.entriesCnt)
	// Walk the bottom level from left to right.
	for move := s.head; move.nexts[0] != nil; move = move.nexts[0] {
		kvs = append(kvs, &KV{
			Key:   move.nexts[0].key,
			Value: move.nexts[0].value,
		})
	}

	return kvs
}

// Size returns the data size in bytes.
func (s *Skiplist) Size() int {
	return s.size
}

// EntriesCnt returns the number of key-value pairs.
func (s *Skiplist) EntriesCnt() int {
	return s.entriesCnt
}

// getNode returns the node matching the key, or nil if not found.
func (s *Skiplist) getNode(key []byte) *skipNode {
	move := s.head
	// Top-down level traversal.
	for level := range slices.Backward(s.head.nexts) {
		// Walk right until the next node is nil or its key >= the search key.
		for move.nexts[level] != nil && bytes.Compare(move.nexts[level].key, key) < 0 {
			move = move.nexts[level]
		}
		// If the next node's key equals the search key, return it. Otherwise descend.
		if move.nexts[level] != nil && bytes.Equal(move.nexts[level].key, key) {
			return move.nexts[level]
		}
	}

	return nil
}

// roll returns a random node height. Minimum is 1; each extra level has probability 1/2.
func (s *Skiplist) roll() int {
	var level int
	randInst := rand.New(rand.NewSource(time.Now().Unix()))
	for randInst.Intn(2) == 1 {
		level++
	}
	return level + 1
}
