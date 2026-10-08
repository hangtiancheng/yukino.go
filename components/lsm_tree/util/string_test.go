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
