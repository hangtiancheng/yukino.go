package lsm_tree

import (
	"bytes"
	"encoding/binary"
	"os"
	"path"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/util"
)

type Index struct {
	Key             []byte
	PrevBlockOffset uint64
	PrevBlockSize   uint64
}

type SSTWriter struct {
	conf          *Config
	dest          *os.File
	dataBuf       *bytes.Buffer
	filterBuf     *bytes.Buffer
	indexBuf      *bytes.Buffer
	blockToFilter map[uint64][]byte
	index         []*Index

	dataBlock     *Block
	filterBlock   *Block
	indexBlock    *Block
	assistScratch [20]byte

	prevKey         []byte
	prevBlockOffset uint64
	prevBlockSize   uint64
}

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

func (s *SSTWriter) Finish() (size uint64, blockToFilter map[uint64][]byte, index []*Index) {
	s.refreshBlock()
	s.insertIndex(s.prevKey)

	_, _ = s.filterBlock.FlushTo(s.filterBuf)
	_, _ = s.indexBlock.FlushTo(s.indexBuf)

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

	_, _ = s.dest.Write(s.dataBuf.Bytes())
	_, _ = s.dest.Write(s.filterBuf.Bytes())
	_, _ = s.dest.Write(s.indexBuf.Bytes())
	_, _ = s.dest.Write(footer)

	blockToFilter = s.blockToFilter
	index = s.index
	return
}

func (s *SSTWriter) Append(key, value []byte) {
	if s.dataBlock.entriesCnt == 0 {
		s.insertIndex(key)
	}

	s.dataBlock.Append(key, value)
	s.conf.Filter.Add(key)
	s.prevKey = key

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
	filterBitmap := s.conf.Filter.Hash()
	s.blockToFilter[s.prevBlockOffset] = filterBitmap
	n := binary.PutUvarint(s.assistScratch[0:], s.prevBlockOffset)
	s.filterBlock.Append(s.assistScratch[:n], filterBitmap)
	s.conf.Filter.Reset()

	s.prevBlockSize, _ = s.dataBlock.FlushTo(s.dataBuf)
}
