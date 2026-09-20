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
	"os"
	"path"
)

// Node represents one sstable in the lsm tree.
type Node struct {
	conf          *Config           // config
	file          string            // sstable file name (without directory)
	level         int               // level of this sstable
	seq           int32             // seq number from the file name level_seq.sst
	size          uint64            // sstable size in bytes
	blockToFilter map[uint64][]byte // filter bitmap per block
	index         []*Index          // index entries per block
	startKey      []byte            // smallest key in the sstable
	endKey        []byte            // largest key in the sstable
	sstReader     *SSTReader        // reader for the sst file
}

func NewNode(conf *Config, file string, sstReader *SSTReader, level int, seq int32, size uint64, blockToFilter map[uint64][]byte, index []*Index) *Node {
	return &Node{
		conf:          conf,
		file:          file,
		sstReader:     sstReader,
		level:         level,
		seq:           seq,
		size:          size,
		blockToFilter: blockToFilter,
		index:         index,
		startKey:      index[0].Key,
		endKey:        index[len(index)-1].Key,
	}
}

func (n *Node) GetAll() ([]*KV, error) {
	return n.sstReader.ReadData()
}

// Get looks up a key in the node.
func (n *Node) Get(key []byte) ([]byte, bool, error) {
	// An empty index means the sstable has no entries.
	if len(n.index) == 0 {
		return nil, false, nil
	}

	// Locate the block via the index.
	index, ok := n.binarySearchIndex(key, 0, len(n.index)-1)
	if !ok {
		return nil, false, nil
	}

	// Use the bloom filter to check if the key may exist.
	bitmap := n.blockToFilter[index.PrevBlockOffset]
	if bitmap == nil {
		return nil, false, nil
	}
	if ok = n.conf.Filter.Exist(bitmap, key); !ok {
		return nil, false, nil
	}

	// Read the block.
	block, err := n.sstReader.ReadBlock(index.PrevBlockOffset, index.PrevBlockSize)
	if err != nil {
		return nil, false, err
	}

	// Parse the block into key-value pairs.
	kvs, err := n.sstReader.ReadBlockData(block)
	if err != nil {
		return nil, false, err
	}

	for _, kv := range kvs {
		if bytes.Equal(kv.Key, key) {
			return kv.Value, true, nil
		}
	}

	return nil, false, nil
}

func (n *Node) Size() uint64 {
	return n.size
}

func (n *Node) Start() []byte {
	return n.startKey
}

func (n *Node) End() []byte {
	return n.endKey
}

func (n *Node) Index() (level int, seq int32) {
	level, seq = n.level, n.seq
	return
}

func (n *Node) Destroy() {
	n.sstReader.Close()
	_ = os.Remove(path.Join(n.conf.Dir, n.file))
}

func (n *Node) Close() {
	n.sstReader.Close()
}

// binarySearchIndex finds the block index that may contain the key.
func (n *Node) binarySearchIndex(key []byte, start, end int) (*Index, bool) {
	if start == end {
		return n.index[start], bytes.Compare(n.index[start].Key, key) >= 0
	}

	// Target block satisfies: key <= index[i].key && key > index[i-1].key
	mid := start + (end-start)>>1
	if bytes.Compare(n.index[mid].Key, key) < 0 {
		return n.binarySearchIndex(key, mid+1, end)
	}

	return n.binarySearchIndex(key, start, mid)
}
