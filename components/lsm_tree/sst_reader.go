package lsm_tree

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path"
	"sync"
)

type KV struct {
	Key   []byte
	Value []byte
}

type SSTReader struct {
	conf         *Config
	src          *os.File
	reader       *bufio.Reader
	filterOffset uint64
	filterSize   uint64
	indexOffset  uint64
	indexSize    uint64

	mu sync.Mutex
}

func NewSSTReader(file string, conf *Config) (*SSTReader, error) {
	if conf == nil || conf.SSTFooterSize < 40 {
		return nil, errors.New("sst reader requires a valid config with a footer size of at least 40 bytes")
	}
	src, err := os.OpenFile(path.Join(conf.Dir, file), os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &SSTReader{
		conf:   conf,
		src:    src,
		reader: bufio.NewReader(src),
	}, nil
}

func (s *SSTReader) Size() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.indexOffset == 0 {
		if err := s.readFooterLocked(); err != nil {
			return 0, err
		}
	}
	return s.indexOffset + s.indexSize, nil
}

func (s *SSTReader) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.reader.Reset(s.src)
	_ = s.src.Close()
}

func (s *SSTReader) ReadFooter() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readFooterLocked()
}

func (s *SSTReader) readFooterLocked() error {
	stat, err := s.src.Stat()
	if err != nil {
		return err
	}
	if s.conf.SSTFooterSize <= 0 || stat.Size() < int64(s.conf.SSTFooterSize) {
		return errors.New("invalid sst footer size")
	}
	if _, err := s.src.Seek(-int64(s.conf.SSTFooterSize), io.SeekEnd); err != nil {
		return err
	}

	s.reader.Reset(s.src)

	if s.filterOffset, err = binary.ReadUvarint(s.reader); err != nil {
		return err
	}

	if s.filterSize, err = binary.ReadUvarint(s.reader); err != nil {
		return err
	}

	if s.indexOffset, err = binary.ReadUvarint(s.reader); err != nil {
		return err
	}

	if s.indexSize, err = binary.ReadUvarint(s.reader); err != nil {
		return err
	}
	fileSize := uint64(stat.Size())
	footerSize := uint64(s.conf.SSTFooterSize)
	if s.filterOffset > s.indexOffset || s.filterSize != s.indexOffset-s.filterOffset ||
		s.indexOffset > fileSize-footerSize || s.indexSize != fileSize-footerSize-s.indexOffset {
		return errors.New("invalid sst section offsets")
	}

	return nil
}

func (s *SSTReader) ReadFilter() (map[uint64][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.filterOffset == 0 || s.filterSize == 0 {
		if err := s.readFooterLocked(); err != nil {
			return nil, err
		}
	}

	filterBlock, err := s.readBlockLocked(s.filterOffset, s.filterSize)
	if err != nil {
		return nil, err
	}

	return s.readFilter(filterBlock)
}

func (s *SSTReader) ReadIndex() ([]*Index, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.indexOffset == 0 || s.indexSize == 0 {
		if err := s.readFooterLocked(); err != nil {
			return nil, err
		}
	}

	indexBlock, err := s.readBlockLocked(s.indexOffset, s.indexSize)
	if err != nil {
		return nil, err
	}

	return s.readIndex(indexBlock)
}

func (s *SSTReader) ReadData() ([]*KV, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.indexOffset == 0 || s.indexSize == 0 || s.filterOffset == 0 || s.filterSize == 0 {
		if err := s.readFooterLocked(); err != nil {
			return nil, err
		}
	}

	dataBlock, err := s.readBlockLocked(0, s.filterOffset)
	if err != nil {
		return nil, err
	}

	return s.ReadBlockData(dataBlock)
}

func (s *SSTReader) ReadBlock(offset, size uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readBlockLocked(offset, size)
}

func (s *SSTReader) readBlockLocked(offset, size uint64) ([]byte, error) {
	if offset > uint64(^uint64(0)>>1) || size > uint64(int(^uint(0)>>1)) {
		return nil, errors.New("sst block is too large")
	}
	stat, err := s.src.Stat()
	if err != nil {
		return nil, err
	}
	if offset > uint64(stat.Size()) || size > uint64(stat.Size())-offset {
		return nil, io.ErrUnexpectedEOF
	}
	if _, err := s.src.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, err
	}
	s.reader.Reset(s.src)

	buf := make([]byte, size)
	_, err = io.ReadFull(s.reader, buf)
	return buf, err
}

func (s *SSTReader) readFilter(block []byte) (map[uint64][]byte, error) {
	blockToFilter := make(map[uint64][]byte)
	buf := bytes.NewBuffer(block)
	var prevKey []byte
	for {
		key, value, err := s.ReadRecord(prevKey, buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		blockOffset, n := binary.Uvarint(key)
		if n <= 0 || n != len(key) {
			return nil, errors.New("invalid sst filter offset")
		}
		blockToFilter[blockOffset] = value
		prevKey = key
	}

	return blockToFilter, nil
}

func (s *SSTReader) readIndex(block []byte) ([]*Index, error) {
	var (
		index   []*Index
		prevKey []byte
	)

	buf := bytes.NewBuffer(block)
	for {
		key, value, err := s.ReadRecord(prevKey, buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		blockOffset, n := binary.Uvarint(value)
		if n <= 0 {
			return nil, errors.New("invalid sst index offset")
		}
		blockSize, m := binary.Uvarint(value[n:])
		if m <= 0 || n+m != len(value) {
			return nil, errors.New("invalid sst index block size")
		}
		index = append(index, &Index{
			Key:             key,
			PrevBlockOffset: blockOffset,
			PrevBlockSize:   blockSize,
		})

		prevKey = key
	}
	return index, nil
}

func (s *SSTReader) ReadBlockData(block []byte) ([]*KV, error) {
	var prevKey []byte
	buf := bytes.NewBuffer(block)
	var data []*KV

	for {
		key, value, err := s.ReadRecord(prevKey, buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		data = append(data, &KV{
			Key:   key,
			Value: value,
		})
		prevKey = key
	}
	return data, nil
}

func (s *SSTReader) ReadRecord(prevKey []byte, buf *bytes.Buffer) (key, value []byte, err error) {
	sharedPrefixLen, err := binary.ReadUvarint(buf)
	if err != nil {
		return nil, nil, err
	}

	keyLen, err := binary.ReadUvarint(buf)
	if err != nil {
		return nil, nil, err
	}

	valLen, err := binary.ReadUvarint(buf)
	if err != nil {
		return nil, nil, err
	}
	if sharedPrefixLen > uint64(len(prevKey)) || keyLen > uint64(buf.Len()) || valLen > uint64(buf.Len())-keyLen ||
		keyLen > uint64(int(^uint(0)>>1)) || valLen > uint64(int(^uint(0)>>1)) {
		return nil, nil, errors.New("invalid sst record lengths")
	}

	key = make([]byte, keyLen)
	if _, err = io.ReadFull(buf, key); err != nil {
		return nil, nil, err
	}

	value = make([]byte, valLen)
	if _, err = io.ReadFull(buf, value); err != nil {
		return nil, nil, err
	}

	sharedPrefix := make([]byte, sharedPrefixLen)
	copy(sharedPrefix, prevKey[:sharedPrefixLen])
	key = append(sharedPrefix, key...)
	return
}
