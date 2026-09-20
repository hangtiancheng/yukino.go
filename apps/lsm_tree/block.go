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
	"io"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/util"
)

// Block is a data block in an sst file. It maps 1:1 to an index entry and a filter entry.
type Block struct {
	conf       *Config       // lsm tree config
	buffer     [30]byte      // scratch buffer for length prefixes
	record     *bytes.Buffer // buffer for accumulating block data
	entriesCnt int           // number of key-value pairs
	prevKey    []byte        // key of the most recently appended entry
}

// NewBlock constructs a Block.
func NewBlock(conf *Config) *Block {
	return &Block{
		conf:   conf,
		record: bytes.NewBuffer([]byte{}),
	}
}

// Append adds a key-value pair to the block.
func (b *Block) Append(key, value []byte) {
	// Always update prevKey and increment the entry count.
	defer func() {
		b.prevKey = append(b.prevKey[:0], key...)
		b.entriesCnt++
	}()

	// Compute the shared-prefix length with the previous key.
	sharedPrefixLen := util.SharedPrefixLen(b.prevKey, key)

	// Encode: shared-key length | remaining-key length | value length
	n := binary.PutUvarint(b.buffer[0:], uint64(sharedPrefixLen))
	n += binary.PutUvarint(b.buffer[n:], uint64(len(key)-sharedPrefixLen))
	n += binary.PutUvarint(b.buffer[n:], uint64(len(value)))

	// Write the length prefixes to the record buffer.
	_, _ = b.record.Write(b.buffer[:n])
	// Write the remaining key and the value.
	b.record.Write(key[sharedPrefixLen:])
	b.record.Write(value)
}

// Size returns the block size in bytes.
func (b *Block) Size() int {
	return b.record.Len()
}

// FlushTo writes the block data to dest and returns the number of bytes written.
func (b *Block) FlushTo(dest io.Writer) (uint64, error) {
	defer b.clear()
	n, err := dest.Write(b.ToBytes())
	return uint64(n), err
}

// ToBytes returns the block data as a byte slice.
func (b *Block) ToBytes() []byte {
	return b.record.Bytes()
}

// clear resets the block for reuse.
func (b *Block) clear() {
	b.entriesCnt = 0
	b.prevKey = b.prevKey[:0]
	b.record.Reset()
}
