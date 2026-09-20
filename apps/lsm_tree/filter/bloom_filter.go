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

package filter

import (
	"errors"
	"hash/fnv"
)

// hashKey computes the 32-bit hash of a key using FNV-1a.
func hashKey(key []byte) uint32 {
	h := fnv.New32a()
	h.Write(key)
	return h.Sum32()
}

// BloomFilter is a bloom filter implementation.
type BloomFilter struct {
	m          int      // bitmap length in bits
	hashedKeys []uint32 // hash values of the keys added to the filter
}

// NewBloomFilter constructs a BloomFilter with the given bitmap length (bits).
func NewBloomFilter(m int) (*BloomFilter, error) {
	if m <= 0 {
		return nil, errors.New("m must be positive")
	}
	return &BloomFilter{
		m: m,
	}, nil
}

// Add inserts a key into the bloom filter.
func (bf *BloomFilter) Add(key []byte) {
	bf.hashedKeys = append(bf.hashedKeys, hashKey(key))
}

// Exist reports whether the key may exist in the filter (false positives are possible).
func (bf *BloomFilter) Exist(bitmap, key []byte) bool {
	// When generating the bitmap, the hash-function count k is stored in the last byte.
	if bitmap == nil {
		bitmap = bf.Hash()
	}
	// Read the hash-function count k.
	k := bitmap[len(bitmap)-1]

	// Base hash h1 = hashKey(key)
	// Base hash h2 = h1 >> 17 | h1 << 15
	// All subsequent hash functions are linearly independent combinations of h1 and h2.
	// i-th hash function gi = h1 + i * h2

	// h1
	hashedKey := hashKey(key)
	// h2
	delta := (hashedKey >> 17) | (hashedKey << 15)
	for i := uint32(0); i < uint32(k); i++ {
		// gi = h1 + i * h2
		targetBit := (hashedKey + i*delta) % uint32(bf.m)
		// Check the target bit; if 0, the key definitely does not exist.
		if bitmap[targetBit>>3]&(1<<(targetBit&7)) == 0 {
			return false
		}
	}

	// All mapped bits are 1: the key probably exists (subject to false-positive rate).
	return true
}

// Hash builds the filter bitmap. The last byte stores k.
func (bf *BloomFilter) Hash() []byte {
	// k: optimal hash-function count derived from m and n.
	k := bf.bestK()
	// Build an empty bitmap with the last byte set to k.
	bitmap := bf.bitmap(k)

	// Base hash h1 = hashKey(key)
	// Base hash h2 = h1 >> 17 | h1 << 15
	// All subsequent hash functions are linearly independent combinations of h1 and h2.
	// i-th hash function gi = h1 + i * h2
	for _, hashedKey := range bf.hashedKeys {
		// hashedKey is h1.
		// delta is h2.
		delta := (hashedKey >> 17) | (hashedKey << 15)
		for i := uint32(0); i < uint32(k); i++ {
			// i-th hash function gi = h1 + i * h2
			// The bit to set.
			targetBit := (hashedKey + i*delta) % uint32(bf.m)
			bitmap[targetBit>>3] |= (1 << (targetBit & 7))
		}
	}

	return bitmap
}

// Reset clears the filter.
func (bf *BloomFilter) Reset() {
	bf.hashedKeys = bf.hashedKeys[:0]
}

// KeyLen returns the number of keys in the filter.
func (bf *BloomFilter) KeyLen() int {
	return len(bf.hashedKeys)
}

// bitmap builds an empty bitmap with the last byte set to k.
func (bf *BloomFilter) bitmap(k uint8) []byte {
	// bytes = ceil(bits / 8)
	bitmapLen := (bf.m + 7) >> 3
	bitmap := make([]byte, bitmapLen+1)
	// The last byte stores k.
	bitmap[bitmapLen] = k
	return bitmap
}

// bestK derives the optimal k from m and n.
func (bf *BloomFilter) bestK() uint8 {
	// Optimal k formula: k = ln2 * m / n  (m = bitmap length, n = key count)
	k := min(
		// k in [1, 30]
		max(

			uint8(69*bf.m/100/len(bf.hashedKeys)), 1), 30)
	return k
}
