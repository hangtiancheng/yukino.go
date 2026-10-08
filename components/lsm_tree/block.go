package lsm_tree

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/util"
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
