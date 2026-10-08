package bloom

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/components/timer/pkg/hash"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/redis"
)

// Filter stores two Murmur3 bit positions in a bounded 2 MiB Redis bitmap.
// At one million entries the approximate false-positive probability is 1.26%.
// A positive result is only a hint; callers must check durable idempotency.
type Filter struct {
	client     *redis.Client
	encryptor1 *hash.Murmur3Encryptor
	encryptor2 *hash.Murmur3AltEncryptor
}

func NewFilter(client *redis.Client, encryptor1 *hash.Murmur3Encryptor, encryptor2 *hash.Murmur3AltEncryptor) *Filter {
	return &Filter{
		client:     client,
		encryptor1: encryptor1,
		encryptor2: encryptor2,
	}
}

func (f *Filter) Exist(ctx context.Context, key, val string) (bool, error) {
	// Check if the value exists in the bloom filter
	rawVal1 := f.encryptor1.Encrypt(val)
	if exist, err := f.client.GetBit(ctx, key, int32(rawVal1%bitmapBits)); err != nil || !exist {
		return exist, err
	}

	rawVal2 := f.encryptor2.Encrypt(val)
	return f.client.GetBit(ctx, key, int32(rawVal2%bitmapBits))
}

func (f *Filter) Set(ctx context.Context, key, val string, expireSeconds int64) error {
	if expireSeconds <= 0 {
		return fmt.Errorf("bloom TTL must be positive")
	}
	rawVal1, rawVal2 := f.encryptor1.Encrypt(val), f.encryptor2.Encrypt(val)
	_, err := f.client.Eval(ctx, `redis.call('SETBIT', KEYS[1], ARGV[1], 1)
redis.call('SETBIT', KEYS[1], ARGV[2], 1)
if redis.call('TTL', KEYS[1]) < 0 then redis.call('EXPIRE', KEYS[1], ARGV[3]) end
return 1`, 1, []any{key, rawVal1 % bitmapBits, rawVal2 % bitmapBits, expireSeconds})
	return err
}

// 2 MiB per daily bitmap. False positives are resolved by durable idempotency.
const bitmapBits = 1 << 24
