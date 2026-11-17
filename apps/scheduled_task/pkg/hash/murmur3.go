package hash

import "github.com/twmb/murmur3"

type Encryptor interface {
	Encrypt(origin string) uint64
}

type Murmur3Encryptor struct{}

func NewMurmur3Encryptor() *Murmur3Encryptor {
	return &Murmur3Encryptor{}
}

func (m *Murmur3Encryptor) Encrypt(origin string) uint64 {
	h1, _ := murmur3.Sum128([]byte(origin))
	return h1
}

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
