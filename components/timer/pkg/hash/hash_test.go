package hash

import (
	"testing"

	"github.com/twmb/murmur3"
)

func TestMurmur3EncryptorDeterministic(t *testing.T) {
	e := NewMurmur3Encryptor()

	first := e.Encrypt("timer")
	for range 3 {
		if got := e.Encrypt("timer"); got != first {
			t.Fatalf("Murmur3Encryptor.Encrypt not deterministic: %d vs %d", got, first)
		}
	}

	if e.Encrypt("a") == e.Encrypt("b") {
		t.Fatal("Murmur3Encryptor.Encrypt collides for distinct inputs a and b")
	}
}

func TestMurmur3EncryptorMatchesLibrary(t *testing.T) {
	e := NewMurmur3Encryptor()
	alt := NewMurmur3AltEncryptor()

	for _, s := range []string{"timer", "", "taskflow:bloom:exec", "<script>alert(1)</script>"} {
		h1, h2 := murmur3.Sum128([]byte(s))
		if got := e.Encrypt(s); got != h1 {
			t.Fatalf("Murmur3Encryptor.Encrypt(%q) = %d, want %d", s, got, h1)
		}
		if got := alt.Encrypt(s); got != h2 {
			t.Fatalf("Murmur3AltEncryptor.Encrypt(%q) = %d, want %d", s, got, h2)
		}
	}
}

func TestMurmur3AltEncryptorDistinct(t *testing.T) {
	e := NewMurmur3Encryptor()
	alt := NewMurmur3AltEncryptor()

	// The two halves of murmur3_x64_128 must differ so the bloom filter gets
	// two independent bit positions.
	for _, s := range []string{"timer", "taskflow", "a"} {
		if e.Encrypt(s) == alt.Encrypt(s) {
			t.Fatalf("primary and alt hashes collide for %q", s)
		}
	}

	first := alt.Encrypt("timer")
	for range 3 {
		if got := alt.Encrypt("timer"); got != first {
			t.Fatalf("Murmur3AltEncryptor.Encrypt not deterministic: %d vs %d", got, first)
		}
	}
}
