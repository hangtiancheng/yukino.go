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

package bloom

import (
	"context"
	"math"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/hash"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/redis"
)

// m: length of the bit vector; backed by Redis, a single bitmap uses a string of up to 512M,
//
//	providing 2^32 bits, so m = 2^32.
//
// n: number of elements in the filter; vectors are isolated per day, assuming 1 million
//
//	execution tasks per day, so n = 10^6.
//
// The false positive probability is (1-e^(-nk/m))^k. With k = 3, it is approximately 2*10^(-10).
// With k = 2, it is approximately 2*10^(-7).
// Therefore, k = 2 is sufficient for the requirements.
// Uses FNV and SHA1 hash functions, with results modulo 2^32 for bit positions.
type Filter struct {
	client     *redis.Client
	encryptor1 *hash.SHA1Encryptor
	encryptor2 *hash.FNVEncryptor
}

func NewFilter(client *redis.Client, encryptor1 *hash.SHA1Encryptor, encryptor2 *hash.FNVEncryptor) *Filter {
	return &Filter{
		client:     client,
		encryptor1: encryptor1,
		encryptor2: encryptor2,
	}
}

func (f *Filter) Exist(ctx context.Context, key, val string) (bool, error) {
	// Check if the value exists in the bloom filter
	rawVal1 := f.encryptor1.Encrypt(val)
	if exist, err := f.client.GetBit(ctx, key, int32(rawVal1%math.MaxInt32)); err != nil || exist {
		return exist, err
	}

	rawVal2 := f.encryptor2.Encrypt(val)
	return f.client.GetBit(ctx, key, int32(rawVal2%math.MaxInt32))
}

func (f *Filter) Set(ctx context.Context, key, val string, expireSeconds int64) error {
	// Check if the key already exists; if not, set the expiration time. Atomicity is not guaranteed here.
	existed, _ := f.client.Exists(ctx, key)

	// Compute the offsets for both hash functions and set the corresponding bits
	rawVal1, rawVal2 := f.encryptor1.Encrypt(val), f.encryptor2.Encrypt(val)
	_, err := f.client.Transaction(ctx, redis.NewSetBitCommand(key, int32(rawVal1%math.MaxInt32), 1),
		redis.NewSetBitCommand(key, int32(rawVal2%math.MaxInt32), 1))

	if !existed {
		_ = f.client.Expire(ctx, key, expireSeconds)
	}
	return err
}
