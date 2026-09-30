package consistent_hash

import (
	"hash/fnv"
	"math"
)

type Encryptor interface {
	Encrypt(origin string) int32
}

// FnvHasher implements Encryptor using the standard library's FNV-1a 32-bit hash.
type FnvHasher struct {
}

func NewFnvHasher() *FnvHasher {
	return &FnvHasher{}
}

func (m *FnvHasher) Encrypt(origin string) int32 {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(origin))
	return int32(hasher.Sum32() % math.MaxInt32)
}
