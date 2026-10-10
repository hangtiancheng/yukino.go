package lsm_tree

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/util"
)

type Block struct {
	conf       *Config
	buffer     [30]byte
	record     *bytes.Buffer
	entriesCnt int
	prevKey    []byte
}

func NewBlock(conf *Config) *Block {
	return &Block{
		conf:   conf,
		record: bytes.NewBuffer([]byte{}),
	}
}

func (b *Block) Append(key, value []byte) {
	defer func() {
		b.prevKey = append(b.prevKey[:0], key...)
		b.entriesCnt++
	}()

	sharedPrefixLen := util.SharedPrefixLen(b.prevKey, key)

	n := binary.PutUvarint(b.buffer[0:], uint64(sharedPrefixLen))
	n += binary.PutUvarint(b.buffer[n:], uint64(len(key)-sharedPrefixLen))
	n += binary.PutUvarint(b.buffer[n:], uint64(len(value)))

	_, _ = b.record.Write(b.buffer[:n])
	b.record.Write(key[sharedPrefixLen:])
	b.record.Write(value)
}

func (b *Block) Size() int {
	return b.record.Len()
}

func (b *Block) FlushTo(dest io.Writer) (uint64, error) {
	defer b.clear()
	n, err := dest.Write(b.ToBytes())
	return uint64(n), err
}

func (b *Block) ToBytes() []byte {
	return b.record.Bytes()
}

func (b *Block) clear() {
	b.entriesCnt = 0
	b.prevKey = b.prevKey[:0]
	b.record.Reset()
}
