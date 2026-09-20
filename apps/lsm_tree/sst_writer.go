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
	"encoding/binary"
	"os"
	"path"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/util"
)

// Index is a block-level index entry used for fast block lookup in an sstable.
type Index struct {
	Key             []byte // separator key; guaranteed >= max key of previous block and < min key of next block
	PrevBlockOffset uint64 // offset of the previous block in the sstable
	PrevBlockSize   uint64 // size of the previous block in bytes
}

// SSTWriter is the write-side view of an sstable.
type SSTWriter struct {
	conf          *Config           // config
	dest          *os.File          // sstable file on disk
	dataBuf       *bytes.Buffer     // data-block buffer: key -> value
	filterBuf     *bytes.Buffer     // filter-block buffer: prev block offset -> filter bitmap
	indexBuf      *bytes.Buffer     // index-block buffer: index key -> prev block offset, prev block size
	blockToFilter map[uint64][]byte // prev block offset -> filter bitmap
	index         []*Index          // index key -> prev block offset, prev block size

	dataBlock     *Block   // data block
	filterBlock   *Block   // filter block
	indexBlock    *Block   // index block
	assistScratch [20]byte // scratch buffer for index-block writes

	prevKey         []byte // key of the most recently appended entry
	prevBlockOffset uint64 // start offset of the previous data block
	prevBlockSize   uint64 // size of the previous data block
}

// NewSSTWriter opens an sstable for writing.
func NewSSTWriter(file string, conf *Config) (*SSTWriter, error) {
	dest, err := os.OpenFile(path.Join(conf.Dir, file), os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &SSTWriter{
		conf:          conf,
		dest:          dest,
		dataBuf:       bytes.NewBuffer([]byte{}),
		filterBuf:     bytes.NewBuffer([]byte{}),
		indexBuf:      bytes.NewBuffer([]byte{}),
		blockToFilter: make(map[uint64][]byte),
		dataBlock:     NewBlock(conf),
		filterBlock:   NewBlock(conf),
		indexBlock:    NewBlock(conf),
		prevKey:       []byte{},
	}, nil
}

// Finish flushes all remaining data to disk and returns metadata for the lsm tree to cache.
func (s *SSTWriter) Finish() (size uint64, blockToFilter map[uint64][]byte, index []*Index) {
	// Flush the last block.
	s.refreshBlock()
	// Append the final index entry.
	s.insertIndex(s.prevKey)

	// Write the filter block to its buffer.
	_, _ = s.filterBlock.FlushTo(s.filterBuf)
	// Write the index block to its buffer.
	_, _ = s.indexBlock.FlushTo(s.indexBuf)

	// Build the footer: filter-block offset, filter-block size, index-block offset, index-block size.
	footer := make([]byte, s.conf.SSTFooterSize)
	size = uint64(s.dataBuf.Len())
	n := binary.PutUvarint(footer[0:], size)
	filterBufLen := uint64(s.filterBuf.Len())
	n += binary.PutUvarint(footer[n:], filterBufLen)
	size += filterBufLen
	n += binary.PutUvarint(footer[n:], size)
	indexBufLen := uint64(s.indexBuf.Len())
	binary.PutUvarint(footer[n:], indexBufLen)
	size += indexBufLen

	// Write everything to the file: data | filter | index | footer.
	_, _ = s.dest.Write(s.dataBuf.Bytes())
	_, _ = s.dest.Write(s.filterBuf.Bytes())
	_, _ = s.dest.Write(s.indexBuf.Bytes())
	_, _ = s.dest.Write(footer)

	blockToFilter = s.blockToFilter
	index = s.index
	return
}

// Append adds a key-value pair to the sstable.
func (s *SSTWriter) Append(key, value []byte) {
	// When starting a new data block, add an index entry.
	if s.dataBlock.entriesCnt == 0 {
		s.insertIndex(key)
	}

	// Append the key-value pair to the data block.
	s.dataBlock.Append(key, value)
	// Add the key to the block's bloom filter.
	s.conf.Filter.Add(key)
	// Track the latest key.
	s.prevKey = key

	// If the data block exceeds the size limit, flush it and start a new block.
	if s.dataBlock.Size() >= s.conf.SSTDataBlockSize {
		s.refreshBlock()
	}
}

func (s *SSTWriter) Size() uint64 {
	return uint64(s.dataBuf.Len())
}

func (s *SSTWriter) Close() {
	_ = s.dest.Close()
	s.dataBuf.Reset()
	s.indexBuf.Reset()
	s.filterBuf.Reset()
}

func (s *SSTWriter) insertIndex(key []byte) {
	// Compute the separator key.
	indexKey := util.GetSeparatorBetween(s.prevKey, key)
	n := binary.PutUvarint(s.assistScratch[0:], s.prevBlockOffset)
	n += binary.PutUvarint(s.assistScratch[n:], s.prevBlockSize)

	s.indexBlock.Append(indexKey, s.assistScratch[:n])
	s.index = append(s.index, &Index{
		Key:             indexKey,
		PrevBlockOffset: s.prevBlockOffset,
		PrevBlockSize:   s.prevBlockSize,
	})
}

func (s *SSTWriter) refreshBlock() {
	if s.conf.Filter.KeyLen() == 0 {
		return
	}

	s.prevBlockOffset = uint64(s.dataBuf.Len())
	// Build the bloom-filter bitmap.
	filterBitmap := s.conf.Filter.Hash()
	s.blockToFilter[s.prevBlockOffset] = filterBitmap
	n := binary.PutUvarint(s.assistScratch[0:], s.prevBlockOffset)
	s.filterBlock.Append(s.assistScratch[:n], filterBitmap)
	// Reset the bloom filter for the next block.
	s.conf.Filter.Reset()

	// Flush the data block to the data buffer.
	s.prevBlockSize, _ = s.dataBlock.FlushTo(s.dataBuf)
}
