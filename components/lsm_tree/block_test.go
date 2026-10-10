package lsm_tree

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func Test_Block_ToBytes(t *testing.T) {
	conf, err := NewConfig("./lsm")
	if err != nil {
		t.Error(err)
		return
	}
	block := NewBlock(conf)
	block.Append([]byte("a"), []byte("b"))
	block.Append([]byte("b"), []byte("c"))
	block.Append([]byte("bcd"), []byte("d"))
	block.Append([]byte("bce"), []byte("e"))

	expect := bytes.NewBuffer([]byte{})
	var recordBuf [8]byte
	n := binary.PutUvarint(recordBuf[0:], uint64(0))
	expect.Write(recordBuf[:n])
	n = binary.PutUvarint(recordBuf[0:], uint64(1))
	expect.Write(recordBuf[:n])
	n = binary.PutUvarint(recordBuf[0:], uint64(1))
	expect.Write(recordBuf[:n])
	expect.Write([]byte{'a', 'b'})
	n = binary.PutUvarint(recordBuf[0:], uint64(0))
	expect.Write(recordBuf[:n])
	n = binary.PutUvarint(recordBuf[0:], uint64(1))
	expect.Write(recordBuf[:n])
	n = binary.PutUvarint(recordBuf[0:], uint64(1))
	expect.Write(recordBuf[:n])
	expect.Write([]byte{'b', 'c'})
	n = binary.PutUvarint(recordBuf[0:], uint64(1))
	expect.Write(recordBuf[:n])
	n = binary.PutUvarint(recordBuf[0:], uint64(2))
	expect.Write(recordBuf[:n])
	n = binary.PutUvarint(recordBuf[0:], uint64(1))
	expect.Write(recordBuf[:n])
	expect.Write([]byte{'c', 'd', 'd'})
	n = binary.PutUvarint(recordBuf[0:], uint64(2))
	expect.Write(recordBuf[:n])
	n = binary.PutUvarint(recordBuf[0:], uint64(1))
	expect.Write(recordBuf[:n])
	n = binary.PutUvarint(recordBuf[0:], uint64(1))
	expect.Write(recordBuf[:n])
	expect.Write([]byte{'e', 'e'})

	if got := block.ToBytes(); !bytes.Equal(got, expect.Bytes()) {
		t.Errorf("expect: %v, got: %v", expect, got)
	}
}
