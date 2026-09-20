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
	"encoding/binary"
	"io"
	"os"
)

// WALWriter appends key-value pairs to a write-ahead log file.
type WALWriter struct {
	file         string   // absolute path to the WAL file
	dest         *os.File // underlying file handle
	assistBuffer [30]byte // scratch buffer for length prefixes
}

// NewWALWriter opens the WAL file for writing, creating it if it does not exist.
// Appends are used so that re-opening an existing WAL (crash recovery of the
// active memtable) never overwrites the records already stored in it.
func NewWALWriter(file string) (*WALWriter, error) {
	dest, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	return &WALWriter{
		file: file,
		dest: dest,
	}, nil
}

// Write appends a key-value pair to the WAL file.
func (w *WALWriter) Write(key, value []byte) error {
	// Encode the key and value lengths into the scratch buffer.
	n := binary.PutUvarint(w.assistBuffer[0:], uint64(len(key)))
	n += binary.PutUvarint(w.assistBuffer[n:], uint64(len(value)))

	// Concatenate: key length | value length | key | value.
	var buf []byte
	buf = append(buf, w.assistBuffer[:n]...)
	buf = append(buf, key...)
	buf = append(buf, value...)
	// Write everything to the WAL file.
	n, err := w.dest.Write(buf)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return io.ErrShortWrite
	}
	return nil
}

func (w *WALWriter) Close() {
	_ = w.dest.Close()
}
