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
	// 0 | 1 | 1 | a | b
	block.Append([]byte("a"), []byte("b"))
	// 0 | 1 | 1 | a | b | 0 | 1 | 1 | b | c
	block.Append([]byte("b"), []byte("c"))
	// 0 | 1 | 1 | a | b | 0 | 1 | 1 | b | c | 1 | 2 | 1 | cd | d
	block.Append([]byte("bcd"), []byte("d"))
	// 0 | 1 | 1 | a | b | 0 | 1 | 1 | b | c | 1 | 2 | 1 | cd | d | 2 | 1 | 1 | e | e
	block.Append([]byte("bce"), []byte("e"))

	expect := bytes.NewBuffer([]byte{})
	// record: 0 | 1 | 1 | a | b | 0 | 1 | 1 | b | c | 1 | 2 | 1 | cd | d | 2 | 1 | 1 | e | e
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
