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

package wal

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/memtable"
)

// WALReader reads a write-ahead log file.
type WALReader struct {
	file   string        // absolute path to the WAL file
	src    *os.File      // underlying file handle
	reader *bufio.Reader // buffered reader wrapping the file
}

// NewWALReader opens the WAL file for reading. The file must already exist.
func NewWALReader(file string) (*WALReader, error) {
	src, err := os.OpenFile(file, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &WALReader{
		file:   file,
		src:    src,
		reader: bufio.NewReader(src),
	}, nil
}

// RestoreToMemtable reads the entire WAL and replays all entries into the given memtable.
func (w *WALReader) RestoreToMemtable(memTable memtable.MemTable) error {
	// Read the full WAL content.
	body, err := io.ReadAll(w.reader)
	if err != nil {
		return err
	}

	// Reset the file offset to the start as a safety net.
	defer func() {
		_, _ = w.src.Seek(0, io.SeekStart)
	}()

	// Parse the raw content into a list of key-value pairs.
	kvs, err := w.readAll(bytes.NewReader(body))
	if err != nil {
		return err
	}

	// Replay each pair into the memtable.
	for _, kv := range kvs {
		memTable.Put(kv.Key, kv.Value)
	}

	return nil
}

// readAll parses the raw WAL content into a list of key-value pairs.
func (w *WALReader) readAll(reader *bytes.Reader) ([]*memtable.KV, error) {
	var kvs []*memtable.KV
	// Loop reading key-value pairs until EOF.
	for {
		// Read the first uvarint as the key length.
		keyLen, err := binary.ReadUvarint(reader)
		// EOF means the file has been fully consumed.
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		// Read the next uvarint as the value length.
		valLen, err := binary.ReadUvarint(reader)
		if err != nil {
			return nil, err
		}

		// Read keyLen bytes as the key.
		keyBuf := make([]byte, keyLen)
		if _, err = io.ReadFull(reader, keyBuf); err != nil {
			return nil, err
		}

		// Read valLen bytes as the value.
		valBuf := make([]byte, valLen)
		if _, err = io.ReadFull(reader, valBuf); err != nil {
			return nil, err
		}

		kvs = append(kvs, &memtable.KV{
			Key:   keyBuf,
			Value: valBuf,
		})
	}

	return kvs, nil
}

func (w *WALReader) Close() {
	w.reader.Reset(w.src)
	_ = w.src.Close()
}
