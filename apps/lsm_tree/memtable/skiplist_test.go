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

package memtable

import (
	"bytes"
	"testing"
)

func Test_Skiplist(t *testing.T) {
	skiplist := NewSkiplist()
	skiplist.Put([]byte("a"), []byte("b"))
	skiplist.Put([]byte("a"), []byte("c"))
	skiplist.Put([]byte("ab"), []byte("aa"))
	skiplist.Put([]byte("abc"), []byte("aaa"))
	skiplist.Put([]byte("bc"), []byte("bbb"))
	skiplist.Put([]byte("ab"), []byte("bb"))

	val, _ := skiplist.Get([]byte("a"))
	if !bytes.Equal(val, []byte("c")) {
		t.Errorf("key: a, expect: c, got: %s", val)
	}
	val, _ = skiplist.Get([]byte("ab"))
	if !bytes.Equal(val, []byte("bb")) {
		t.Errorf("key: ab, expect: bb, got: %s", val)
	}
	val, _ = skiplist.Get([]byte("abc"))
	if !bytes.Equal(val, []byte("aaa")) {
		t.Errorf("key: abc, expect: aaa, got: %s", val)
	}
	val, _ = skiplist.Get([]byte("bc"))
	if !bytes.Equal(val, []byte("bbb")) {
		t.Errorf("key: bc, expect: bbb, got: %s", val)
	}
	_, ok := skiplist.Get([]byte("bcd"))
	if ok {
		t.Errorf("key: bcd, expect ok: false, got: true")
	}
	if skiplist.EntriesCnt() != 4 {
		t.Errorf("entriesCnt, expect: 4, got: %d", skiplist.EntriesCnt())
	}
	if skiplist.Size() != 17 {
		t.Errorf("size, expect: 17, got: %d", skiplist.Size())
	}

	kvs := skiplist.All()
	if len(kvs) != 4 {
		t.Errorf("kvs len, expect: 4, got: %d", len(kvs))
	}

	if !bytes.Equal(kvs[0].Key, []byte("a")) {
		t.Errorf("kvs[0] key, expect: a, got: %s", kvs[0].Key)
	}
	if !bytes.Equal(kvs[0].Value, []byte("c")) {
		t.Errorf("kvs[0] value, expect: c, got: %s", kvs[0].Value)
	}

	if !bytes.Equal(kvs[1].Key, []byte("ab")) {
		t.Errorf("kvs[1] key, expect: ab, got: %s", kvs[1].Key)
	}
	if !bytes.Equal(kvs[1].Value, []byte("bb")) {
		t.Errorf("kvs[1] value, expect: bb, got: %s", kvs[1].Value)
	}

	if !bytes.Equal(kvs[2].Key, []byte("abc")) {
		t.Errorf("kvs[2] key, expect: abc, got: %s", kvs[2].Key)
	}
	if !bytes.Equal(kvs[2].Value, []byte("aaa")) {
		t.Errorf("kvs[2] value, expect: aaa, got: %s", kvs[2].Value)
	}

	if !bytes.Equal(kvs[3].Key, []byte("bc")) {
		t.Errorf("kvs[3] key, expect: bc, got: %s", kvs[3].Key)
	}
	if !bytes.Equal(kvs[3].Value, []byte("bbb")) {
		t.Errorf("kvs[3] value, expect: bbb, got: %s", kvs[3].Value)
	}
}
