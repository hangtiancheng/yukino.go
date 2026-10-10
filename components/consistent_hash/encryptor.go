package consistent_hash

import (
	"hash/fnv"
	"math"
)

type Encryptor interface {
	Encrypt(origin string) int32
}

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
