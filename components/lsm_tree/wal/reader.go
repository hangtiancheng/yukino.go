package wal

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/memtable"
)

type WALReader struct {
	file   string
	src    *os.File
	reader *bufio.Reader
}

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

func (w *WALReader) RestoreToMemtable(memTable memtable.MemTable) error {
	if memTable == nil {
		return errors.New("nil memtable")
	}
	body, err := io.ReadAll(w.reader)
	if err != nil {
		return err
	}

	defer func() {
		_, _ = w.src.Seek(0, io.SeekStart)
	}()

	kvs, validSize, err := w.readAll(bytes.NewReader(body))
	if err != nil {
		return err
	}
	if validSize < int64(len(body)) {
		if err = os.Truncate(w.file, validSize); err != nil {
			return err
		}
	}

	for _, kv := range kvs {
		memTable.Put(kv.Key, kv.Value)
	}

	return nil
}

func (w *WALReader) readAll(reader *bytes.Reader) ([]*memtable.KV, int64, error) {
	var kvs []*memtable.KV
	var validSize int64
	for {
		keyLen, err := binary.ReadUvarint(reader)
		if err != nil {
			if errors.Is(err, io.EOF) && reader.Len() == 0 {
				return kvs, validSize, nil
			}
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return kvs, validSize, nil
			}
			return nil, validSize, err
		}

		valLen, err := binary.ReadUvarint(reader)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return kvs, validSize, nil
			}
			return nil, validSize, err
		}
		if keyLen > uint64(reader.Len()) || valLen > uint64(reader.Len())-keyLen ||
			keyLen > uint64(int(^uint(0)>>1)) || valLen > uint64(int(^uint(0)>>1)) {
			return kvs, validSize, nil
		}

		keyBuf := make([]byte, keyLen)
		if _, err = io.ReadFull(reader, keyBuf); err != nil {
			return kvs, validSize, nil
		}

		valBuf := make([]byte, valLen)
		if _, err = io.ReadFull(reader, valBuf); err != nil {
			return kvs, validSize, nil
		}

		kvs = append(kvs, &memtable.KV{
			Key:   keyBuf,
			Value: valBuf,
		})
		validSize = reader.Size() - int64(reader.Len())
	}
}

func (w *WALReader) Close() {
	w.reader.Reset(w.src)
	_ = w.src.Close()
}
