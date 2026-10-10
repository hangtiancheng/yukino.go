package wal

import (
	"encoding/binary"
	"io"
	"os"
)

type WALWriter struct {
	file         string
	dest         *os.File
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
	n := binary.PutUvarint(w.assistBuffer[0:], uint64(len(key)))
	n += binary.PutUvarint(w.assistBuffer[n:], uint64(len(value)))

	var buf []byte
	buf = append(buf, w.assistBuffer[:n]...)
	buf = append(buf, key...)
	buf = append(buf, value...)
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
