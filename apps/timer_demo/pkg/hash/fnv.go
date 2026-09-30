package hash

import (
	"hash/fnv"
)

type FNVEncryptor struct {
}

func NewMurmur3Encryptor() *FNVEncryptor {
	return &FNVEncryptor{}
}

func (m *FNVEncryptor) Encrypt(origin string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(origin))
	return h.Sum64()
}
