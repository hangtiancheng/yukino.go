package hash

import "testing"

func TestSHA1EncryptorDeterministic(t *testing.T) {
	e := NewSHA1Encryptor()

	first := e.Encrypt("timer_demo")
	for range 3 {
		if got := e.Encrypt("timer_demo"); got != first {
			t.Fatalf("SHA1Encryptor.Encrypt not deterministic: %d vs %d", got, first)
		}
	}

	if e.Encrypt("a") == e.Encrypt("b") {
		t.Fatal("SHA1Encryptor.Encrypt collides for distinct inputs a and b")
	}
}

func TestFNVEncryptorDeterministic(t *testing.T) {
	e := NewMurmur3Encryptor()

	first := e.Encrypt("timer_demo")
	for i := 0; i < 3; i++ {
		if got := e.Encrypt("timer_demo"); got != first {
			t.Fatalf("FNVEncryptor.Encrypt not deterministic: %d vs %d", got, first)
		}
	}

	if e.Encrypt("a") == e.Encrypt("b") {
		t.Fatal("FNVEncryptor.Encrypt collides for distinct inputs a and b")
	}
}
