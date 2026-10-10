package filter

import (
	"errors"
	"hash/fnv"
)

func hashKey(key []byte) uint32 {
	h := fnv.New32a()
	h.Write(key)
	return h.Sum32()
}

type BloomFilter struct {
	m          int
	hashedKeys []uint32
}

func NewBloomFilter(m int) (*BloomFilter, error) {
	if m <= 0 {
		return nil, errors.New("m must be positive")
	}
	return &BloomFilter{
		m: m,
	}, nil
}

func (bf *BloomFilter) Add(key []byte) {
	bf.hashedKeys = append(bf.hashedKeys, hashKey(key))
}

func (bf *BloomFilter) Exist(bitmap, key []byte) bool {
	if bitmap == nil {
		bitmap = bf.Hash()
	}
	k := bitmap[len(bitmap)-1]

	hashedKey := hashKey(key)
	delta := (hashedKey >> 17) | (hashedKey << 15)
	for i := uint32(0); i < uint32(k); i++ {
		targetBit := (hashedKey + i*delta) % uint32(bf.m)
		if bitmap[targetBit>>3]&(1<<(targetBit&7)) == 0 {
			return false
		}
	}

	return true
}

func (bf *BloomFilter) Hash() []byte {
	k := bf.bestK()
	bitmap := bf.bitmap(k)

	for _, hashedKey := range bf.hashedKeys {
		delta := (hashedKey >> 17) | (hashedKey << 15)
		for i := uint32(0); i < uint32(k); i++ {
			targetBit := (hashedKey + i*delta) % uint32(bf.m)
			bitmap[targetBit>>3] |= (1 << (targetBit & 7))
		}
	}

	return bitmap
}

func (bf *BloomFilter) Reset() {
	bf.hashedKeys = bf.hashedKeys[:0]
}

func (bf *BloomFilter) KeyLen() int {
	return len(bf.hashedKeys)
}

func (bf *BloomFilter) bitmap(k uint8) []byte {
	bitmapLen := (bf.m + 7) >> 3
	bitmap := make([]byte, bitmapLen+1)
	bitmap[bitmapLen] = k
	return bitmap
}

func (bf *BloomFilter) bestK() uint8 {
	k := min(
		max(

			uint8(69*bf.m/100/len(bf.hashedKeys)), 1), 30)
	return k
}
