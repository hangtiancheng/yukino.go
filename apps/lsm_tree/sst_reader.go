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
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path"
	"sync"
)

// KV is a key-value pair.
type KV struct {
	Key   []byte
	Value []byte
}

// SSTReader is the read-side view of an sstable.
type SSTReader struct {
	conf         *Config       // config
	src          *os.File      // underlying file
	reader       *bufio.Reader // buffered reader
	filterOffset uint64        // filter-block offset within the sstable
	filterSize   uint64        // filter-block size in bytes
	indexOffset  uint64        // index-block offset within the sstable
	indexSize    uint64        // index-block size in bytes

	// mu serializes file reads: the file offset and buffered reader are shared
	// state, and readers may run concurrently (per-level locks are read locks).
	mu sync.Mutex
}

// NewSSTReader opens an sstable for reading.
func NewSSTReader(file string, conf *Config) (*SSTReader, error) {
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

// Size returns the sstable data size in bytes.
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

// ReadFooter reads the sstable footer and populates the reader fields.
func (s *SSTReader) ReadFooter() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readFooterLocked()
}

// readFooterLocked reads the sstable footer; s.mu must be held.
func (s *SSTReader) readFooterLocked() error {
	// Seek back from the end by the footer size.
	if _, err := s.src.Seek(-int64(s.conf.SSTFooterSize), io.SeekEnd); err != nil {
		return err
	}

	s.reader.Reset(s.src)

	var err error
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

	return nil
}

// ReadFilter reads the filter block.
func (s *SSTReader) ReadFilter() (map[uint64][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load the footer first if needed.
	if s.filterOffset == 0 || s.filterSize == 0 {
		if err := s.readFooterLocked(); err != nil {
			return nil, err
		}
	}

	// Read the filter-block content.
	filterBlock, err := s.readBlockLocked(s.filterOffset, s.filterSize)
	if err != nil {
		return nil, err
	}

	// Parse the filter block.
	return s.readFilter(filterBlock)
}

// ReadIndex reads the index block.
func (s *SSTReader) ReadIndex() ([]*Index, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load the footer first if needed.
	if s.indexOffset == 0 || s.indexSize == 0 {
		if err := s.readFooterLocked(); err != nil {
			return nil, err
		}
	}

	// Read the index-block content.
	indexBlock, err := s.readBlockLocked(s.indexOffset, s.indexSize)
	if err != nil {
		return nil, err
	}

	// Parse the index block.
	return s.readIndex(indexBlock)
}

// ReadData reads all key-value pairs from the sstable.
func (s *SSTReader) ReadData() ([]*KV, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load the footer first if needed.
	if s.indexOffset == 0 || s.indexSize == 0 || s.filterOffset == 0 || s.filterSize == 0 {
		if err := s.readFooterLocked(); err != nil {
			return nil, err
		}
	}

	// Read all data blocks.
	dataBlock, err := s.readBlockLocked(0, s.filterOffset)
	if err != nil {
		return nil, err
	}

	// Parse all data blocks.
	return s.ReadBlockData(dataBlock)
}

// ReadBlock reads a block at the given offset and size.
func (s *SSTReader) ReadBlock(offset, size uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readBlockLocked(offset, size)
}

// readBlockLocked reads a block at the given offset and size; s.mu must be held.
func (s *SSTReader) readBlockLocked(offset, size uint64) ([]byte, error) {
	// Seek to the start offset.
	if _, err := s.src.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, err
	}
	s.reader.Reset(s.src)

	// Read exactly size bytes.
	buf := make([]byte, size)
	_, err := io.ReadFull(s.reader, buf)
	return buf, err
}

// readFilter parses the filter-block content.
func (s *SSTReader) readFilter(block []byte) (map[uint64][]byte, error) {
	blockToFilter := make(map[uint64][]byte)
	// Wrap the block in a buffer.
	buf := bytes.NewBuffer(block)
	var prevKey []byte
	for {
		// Each record: key = block offset, value = filter bitmap.
		key, value, err := s.ReadRecord(prevKey, buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		blockOffset, _ := binary.Uvarint(key)
		blockToFilter[blockOffset] = value
		prevKey = key
	}

	return blockToFilter, nil
}

// readIndex parses the index-block content.
func (s *SSTReader) readIndex(block []byte) ([]*Index, error) {
	var (
		index   []*Index
		prevKey []byte
	)

	// Wrap the block in a buffer.
	buf := bytes.NewBuffer(block)
	for {
		// Each record: key = separator key, value = previous block's offset and size.
		key, value, err := s.ReadRecord(prevKey, buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		blockOffset, n := binary.Uvarint(value)
		blockSize, _ := binary.Uvarint(value[n:])
		index = append(index, &Index{
			Key:             key,
			PrevBlockOffset: blockOffset,
			PrevBlockSize:   blockSize,
		})

		prevKey = key
	}
	return index, nil
}

// ReadBlockData parses a data block into key-value pairs.
func (s *SSTReader) ReadBlockData(block []byte) ([]*KV, error) {
	// Track the previous key for prefix sharing.
	var prevKey []byte
	// Wrap the block in a buffer.
	buf := bytes.NewBuffer(block)
	var data []*KV

	for {
		// Read one key-value pair.
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
		// Update prevKey.
		prevKey = key
	}
	return data, nil
}

// ReadRecord reads a single key-value record from buf.
func (s *SSTReader) ReadRecord(prevKey []byte, buf *bytes.Buffer) (key, value []byte, err error) {
	// Read the shared-prefix length with the previous key.
	sharedPrefixLen, err := binary.ReadUvarint(buf)
	if err != nil {
		return nil, nil, err
	}

	// Read the remaining key length.
	keyLen, err := binary.ReadUvarint(buf)
	if err != nil {
		return nil, nil, err
	}

	// Read the value length.
	valLen, err := binary.ReadUvarint(buf)
	if err != nil {
		return nil, nil, err
	}

	// Read the remaining key.
	key = make([]byte, keyLen)
	if _, err = io.ReadFull(buf, key); err != nil {
		return nil, nil, err
	}

	// Read the value.
	value = make([]byte, valLen)
	if _, err = io.ReadFull(buf, value); err != nil {
		return nil, nil, err
	}

	// Reconstruct the full key = shared prefix + remaining key.
	sharedPrefix := make([]byte, sharedPrefixLen)
	copy(sharedPrefix, prevKey[:sharedPrefixLen])
	key = append(sharedPrefix, key...)
	return
}
