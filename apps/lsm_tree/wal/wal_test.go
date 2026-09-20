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
	"bytes"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/memtable"
)

func Test_WAL(t *testing.T) {
	walWriter, err := NewWALWriter("./test.wal")
	if err != nil {
		t.Error(err)
		return
	}
	defer walWriter.Close()

	skiplist := memtable.NewSkiplist()

	kvs := make([]*memtable.KV, 0, 100)
	for i := range 100 {
		kvs = append(kvs, &memtable.KV{
			Key:   []byte{'a' + uint8(i)},
			Value: []byte{'b' + uint8(i)},
		})
	}

	for _, kv := range kvs {
		skiplist.Put(kv.Key, kv.Value)
		if err = walWriter.Write(kv.Key, kv.Value); err != nil {
			t.Error(err)
			return
		}
	}

	walReader, err := NewWALReader("./test.wal")
	if err != nil {
		t.Error(err)
		return
	}
	defer walReader.Close()

	restoredSkiplist := memtable.NewSkiplist()
	walReader.RestoreToMemtable(restoredSkiplist)

	originKVs := skiplist.All()
	restoredKVs := restoredSkiplist.All()

	if len(originKVs) != len(restoredKVs) {
		t.Errorf("not equal len, got: %d, expect: %d", len(restoredKVs), len(originKVs))
		return
	}

	for i := range originKVs {
		if !bytes.Equal(originKVs[i].Key, restoredKVs[i].Key) {
			t.Errorf("not equal, index: %d, got key: %s, expect: %s", i, restoredKVs[i].Key, originKVs[i].Key)
		}
		if !bytes.Equal(originKVs[i].Value, restoredKVs[i].Value) {
			t.Errorf("not equal, index: %d, got val: %s, expect: %s", i, restoredKVs[i].Value, originKVs[i].Value)
		}
	}
}
