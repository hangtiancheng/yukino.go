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
