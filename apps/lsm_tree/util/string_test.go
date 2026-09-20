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

package util

// cSpell: words abce
import (
	"bytes"
	"testing"
)

func Test_SharedPrefixLen(t *testing.T) {
	if got := SharedPrefixLen([]byte("a"), nil); got != 0 {
		t.Errorf("SharedPrefixLen(a, nil), expect: 0, got: %d", got)
	}
	if got := SharedPrefixLen([]byte("ab"), []byte("abc")); got != 2 {
		t.Errorf("SharedPrefixLen(ab, abc), expect: 2, got: %d", got)
	}
	if got := SharedPrefixLen([]byte("ab"), []byte("c")); got != 0 {
		t.Errorf("SharedPrefixLen(ab, c), expect: 0, got: %d", got)
	}
}

func Test_GetSeparatorBetween(t *testing.T) {
	if got := GetSeparatorBetween(nil, []byte("b")); !bytes.Equal(got, []byte("a")) {
		t.Errorf("GetSeparatorBetween(nil, b), expect: a, got: %s", got)
	}
	if got := GetSeparatorBetween([]byte("abcd"), []byte("abcde")); !bytes.Equal(got, []byte("abcd")) {
		t.Errorf("GetSeparatorBetween(abcd, abcde), expect: abcd, got: %s", got)
	}
	if got := GetSeparatorBetween([]byte("abcd"), []byte("abce")); !bytes.Equal(got, []byte("abcd")) {
		t.Errorf("GetSeparatorBetween(abcd, abce), expect: abcd, got: %s", got)
	}
	// An empty b must not panic with a negative slice bound.
	if got := GetSeparatorBetween(nil, nil); len(got) != 0 {
		t.Errorf("GetSeparatorBetween(nil, nil), expect empty, got: %s", got)
	}
	if got := GetSeparatorBetween([]byte("a"), nil); len(got) != 0 {
		t.Errorf("GetSeparatorBetween(a, nil), expect empty, got: %s", got)
	}
}
