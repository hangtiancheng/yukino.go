package lsm_tree

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
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
	finishErr       error
	finished        bool
}

func NewSSTWriter(file string, conf *Config) (*SSTWriter, error) {
	if conf == nil || conf.SSTFooterSize < 40 {
		return nil, errors.New("sst writer requires a valid config with a footer size of at least 40 bytes")
	}
	dest, err := os.OpenFile(path.Join(conf.Dir, file), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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
	if s.finished {
		return 0, s.blockToFilter, s.index
	}
	s.finished = true
	s.refreshBlock()
	s.insertIndex(s.prevKey)

	_, s.finishErr = s.filterBlock.FlushTo(s.filterBuf)
	if s.finishErr == nil {
		_, s.finishErr = s.indexBlock.FlushTo(s.indexBuf)
	}
	if s.finishErr != nil {
		return 0, s.blockToFilter, s.index
	}

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

	for _, section := range [][]byte{s.dataBuf.Bytes(), s.filterBuf.Bytes(), s.indexBuf.Bytes(), footer} {
		if s.finishErr = writeFull(s.dest, section); s.finishErr != nil {
			return 0, s.blockToFilter, s.index
		}
	}
	if s.finishErr = s.dest.Sync(); s.finishErr != nil {
		return 0, s.blockToFilter, s.index
	}

	blockToFilter = s.blockToFilter
	index = s.index
	return
}

func (s *SSTWriter) Err() error {
	return s.finishErr
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func (s *SSTWriter) Append(key, value []byte) {
	if s.dataBlock.entriesCnt == 0 {
		s.insertIndex(key)
	}

	s.dataBlock.Append(key, value)
	s.conf.Filter.Add(key)
	s.prevKey = bytes.Clone(key)

	if s.dataBlock.Size() >= s.conf.SSTDataBlockSize {
		s.refreshBlock()
	}
}

func (s *SSTWriter) Size() uint64 {
	return uint64(s.dataBuf.Len())
}

func (s *SSTWriter) Close() error {
	var errs []error
	if s.dest != nil {
		file := s.dest.Name()
		if err := s.dest.Close(); err != nil {
			errs = append(errs, err)
		}
		s.dest = nil
		if !s.finished || s.finishErr != nil {
			if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	s.dataBuf.Reset()
	s.indexBuf.Reset()
	s.filterBuf.Reset()
	return errors.Join(errs...)
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
