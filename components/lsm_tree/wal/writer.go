package wal

import (
	"encoding/binary"
	"io"
	"os"
	"sync"
)

type WALWriter struct {
	file         string
	dest         *os.File
	mu           sync.Mutex
	closed       bool
	assistBuffer [30]byte
}

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

func (w *WALWriter) Write(key, value []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return os.ErrClosed
	}
	n := binary.PutUvarint(w.assistBuffer[0:], uint64(len(key)))
	n += binary.PutUvarint(w.assistBuffer[n:], uint64(len(value)))

	var buf []byte
	buf = append(buf, w.assistBuffer[:n]...)
	buf = append(buf, key...)
	buf = append(buf, value...)
	for len(buf) > 0 {
		written, err := w.dest.Write(buf)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		buf = buf[written:]
	}
	return w.dest.Sync()
}

func (w *WALWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.dest.Close()
}
