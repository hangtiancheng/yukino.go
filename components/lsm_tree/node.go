package lsm_tree

import (
	"bytes"
	"os"
	"path"
)

type Node struct {
	conf          *Config
	file          string
	level         int
	seq           int32
	size          uint64
	blockToFilter map[uint64][]byte
	index         []*Index
	startKey      []byte
	endKey        []byte
	sstReader     *SSTReader
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

func (n *Node) Get(key []byte) ([]byte, bool, error) {
	if len(n.index) == 0 {
		return nil, false, nil
	}

	index, ok := n.binarySearchIndex(key, 0, len(n.index)-1)
	if !ok {
		return nil, false, nil
	}

	bitmap := n.blockToFilter[index.PrevBlockOffset]
	if bitmap == nil {
		return nil, false, nil
	}
	if ok = n.conf.Filter.Exist(bitmap, key); !ok {
		return nil, false, nil
	}

	block, err := n.sstReader.ReadBlock(index.PrevBlockOffset, index.PrevBlockSize)
	if err != nil {
		return nil, false, err
	}

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

func (n *Node) binarySearchIndex(key []byte, start, end int) (*Index, bool) {
	if start == end {
		return n.index[start], bytes.Compare(n.index[start].Key, key) >= 0
	}

	mid := start + (end-start)>>1
	if bytes.Compare(n.index[mid].Key, key) < 0 {
		return n.binarySearchIndex(key, mid+1, end)
	}

	return n.binarySearchIndex(key, start, mid)
}
