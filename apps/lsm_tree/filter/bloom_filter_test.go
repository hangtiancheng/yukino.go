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

package filter

import (
	"testing"
)

func Test_BloomFilter_Add_Exist(t *testing.T) {
	m := 16
	bf, err := NewBloomFilter(m)
	if err != nil {
		t.Error(err)
		return
	}

	bf.Add([]byte("a"))
	bf.Add([]byte("b"))
	bf.Add([]byte("c"))
	bf.Add([]byte("d"))

	bitmap := bf.Hash()
	if ok := bf.Exist(bitmap, []byte("a")); !ok {
		t.Errorf("key: %v, expect: true, got: false", "a")
	}

	if ok := bf.Exist(bitmap, []byte("b")); !ok {
		t.Errorf("key: %v, expect: true, got: false", "b")
	}

	if ok := bf.Exist(bitmap, []byte("c")); !ok {
		t.Errorf("key: %v, expect: true, got: false", "c")
	}

	if ok := bf.Exist(bitmap, []byte("d")); !ok {
		t.Errorf("key: %v, expect: true, got: false", "d")
	}

	if ok := bf.Exist(bitmap, []byte("e")); ok {
		t.Errorf("key: %v, expect: false, got: true", "e")
	}
}

func Test_BloomFilter_Hash(t *testing.T) {
	m := 8
	bf, err := NewBloomFilter(m)
	if err != nil {
		t.Error(err)
		return
	}

	bf.Add([]byte("a"))
	bf.Add([]byte("b"))

	bitmap := bf.Hash()
	// bitmap length = ceil(m/8) + 1 = 2 (one data byte + one byte for k)
	if len(bitmap) != 2 {
		t.Errorf("bitmap len, expect: 2, got: %d", len(bitmap))
	}

	// Optimal k = ln2 * m / n = 69 * 8 / 100 / 2 = 2
	if k := bitmap[len(bitmap)-1]; k != 2 {
		t.Errorf("k, expect: 2, got: %d", k)
	}

	// Added keys must be reported as existing.
	for _, key := range [][]byte{[]byte("a"), []byte("b")} {
		if !bf.Exist(bitmap, key) {
			t.Errorf("key: %s, expect: true, got: false", key)
		}
	}
}

func Test_bitOperation(t *testing.T) {
	hashedKey1_1 := hashKey([]byte("a"))
	t.Log(hashedKey1_1)
	t.Log(hashedKey1_1 & 7)
	hashedKey1_2 := hashedKey1_1 + (hashedKey1_1 >> 17) | (hashedKey1_1 << 15)
	t.Log(hashedKey1_2)
	t.Log(hashedKey1_2 & 7)
	hashedKey2_1 := hashKey([]byte("b"))
	t.Log(hashedKey2_1)
	t.Log(hashedKey2_1 & 7)
	hashedKey2_2 := hashedKey2_1 + (hashedKey2_1 >> 17) | (hashedKey2_1 << 15)
	t.Log(hashedKey2_2)
	t.Log(hashedKey2_2 & 7)
}
