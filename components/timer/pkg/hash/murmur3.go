// Package hash provides the 64-bit string hashers used by the timer service
// (bloom filter bit positions, shard keys), backed by the open-source
// twmb/murmur3 implementation.
package hash

import "github.com/twmb/murmur3"

// Encryptor hashes a string into a uint64. Both murmur3 encryptors satisfy
// it; the bloom filter references them by concrete type so the dig container
// can resolve each one unambiguously.
type Encryptor interface {
	Encrypt(origin string) uint64
}

// Murmur3Encryptor is the primary murmur3 hasher: the first 64-bit half of
// murmur3_x64_128.
type Murmur3Encryptor struct{}

func NewMurmur3Encryptor() *Murmur3Encryptor {
	return &Murmur3Encryptor{}
}

func (m *Murmur3Encryptor) Encrypt(origin string) uint64 {
	h1, _ := murmur3.Sum128([]byte(origin))
	return h1
}

// Murmur3AltEncryptor is the secondary murmur3 hasher: the second 64-bit half
// of murmur3_x64_128, giving the bloom filter a second independent bit
// position from the same hashing scheme.
type Murmur3AltEncryptor struct{}

func NewMurmur3AltEncryptor() *Murmur3AltEncryptor {
	return &Murmur3AltEncryptor{}
}

func (m *Murmur3AltEncryptor) Encrypt(origin string) uint64 {
	_, h2 := murmur3.Sum128([]byte(origin))
	return h2
}

var (
	_ Encryptor = (*Murmur3Encryptor)(nil)
	_ Encryptor = (*Murmur3AltEncryptor)(nil)
)
