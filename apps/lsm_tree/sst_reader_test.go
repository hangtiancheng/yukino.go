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
	"fmt"
	"testing"
)

func Test_SSTReader(t *testing.T) {
	// Build an sst writer and write data.
	conf, err := NewConfig("./lsm", WithSSTDataBlockSize(16))
	if err != nil {
		t.Error(err)
		return
	}
	sstWriter, err := NewSSTWriter("test_write_read.sst", conf)
	if err != nil {
		t.Error(err)
		return
	}
	defer sstWriter.Close()

	// datablock1: record: [0 1 1 a b] [1 1 2 b c d] [0 1 1 e f]
	// datablock2: record: [0 2 1 e f g h]
	// filter: 0 -> bitmap1  16 -> bitmap2
	// index: [` 0 0] [e 0 16] [ef 16 7]
	// footer: ...
	expectKvs := []*KV{
		{
			Key:   []byte("a"),
			Value: []byte("b"),
		},
		{
			Key:   []byte("ab"),
			Value: []byte("cd"),
		},
		{
			Key:   []byte("e"),
			Value: []byte("f"),
		},
		{
			Key:   []byte("ef"),
			Value: []byte("gh"),
		},
	}

	for _, kv := range expectKvs {
		sstWriter.Append(kv.Key, kv.Value)
	}

	_, expectBlockToFilter, expectIndex := sstWriter.Finish()

	// Build an sst reader and read data.
	sstReader, err := NewSSTReader("test_write_read.sst", conf)
	if err != nil {
		t.Error(err)
		return
	}
	defer sstReader.Close()

	gotBlockToFilter, err := sstReader.ReadFilter()
	if err != nil {
		t.Error(err)
		return
	}

	gotIndex, err := sstReader.ReadIndex()
	if err != nil {
		t.Error(err)
		return
	}

	if err = assertFilterEqual(expectBlockToFilter, gotBlockToFilter); err != nil {
		t.Error(err)
		return
	}

	if err = assertIndexEqual(expectIndex, gotIndex); err != nil {
		t.Error(err)
		return
	}

	gotKVs, err := sstReader.ReadData()
	if err != nil {
		t.Error(err)
		return
	}

	if err = assertDataEqual(expectKvs, gotKVs); err != nil {
		t.Error(err)
	}
}

func assertFilterEqual(expect, got map[uint64][]byte) error {
	if len(expect) != len(got) {
		return fmt.Errorf("expect len: %d, got len: %d", len(expect), len(got))
	}

	for expectK, expectV := range expect {
		gotV := got[expectK]
		if !bytes.Equal(expectV, gotV) {
			return fmt.Errorf("key: %d, expect v: %s, got v: %s", expectK, expectV, gotV)
		}
	}

	return nil
}

func assertIndexEqual(expect, got []*Index) error {
	if len(expect) != len(got) {
		return fmt.Errorf("expect len: %d, got len: %d", len(expect), len(got))
	}

	for i := range expect {
		if !bytes.Equal(expect[i].Key, got[i].Key) {
			return fmt.Errorf("index: %d, expect key: %s, got key: %s", i, expect[i].Key, got[i].Key)
		}

		if expect[i].PrevBlockOffset != got[i].PrevBlockOffset {
			return fmt.Errorf("index: %d, expect offset: %d, got offset: %d", i, expect[i].PrevBlockOffset, got[i].PrevBlockOffset)
		}

		if expect[i].PrevBlockSize != got[i].PrevBlockSize {
			return fmt.Errorf("index: %d, expect size: %d, got size: %d", i, expect[i].PrevBlockSize, got[i].PrevBlockSize)
		}
	}
	return nil
}

func assertDataEqual(expect, got []*KV) error {
	if len(expect) != len(got) {
		return fmt.Errorf("expect len: %d, got len: %d", len(expect), len(got))
	}

	for i := range expect {
		if !bytes.Equal(expect[i].Key, got[i].Key) {
			return fmt.Errorf("data: %d, data key: %s, got key: %s", i, expect[i].Key, got[i].Key)
		}

		if !bytes.Equal(expect[i].Value, got[i].Value) {
			return fmt.Errorf("data: %d, expect offset: %d, got offset: %d", i, expect[i].Value, got[i].Value)
		}

	}
	return nil
}
