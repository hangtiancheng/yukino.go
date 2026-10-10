package idem

import (
	"context"
	"log/slog"
	"time"

	timerconf "github.com/hangtiancheng/yukino.go/components/timer/common/conf"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/bloom"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/hash"
	timerredis "github.com/hangtiancheng/yukino.go/components/timer/pkg/redis"
	"github.com/redis/go-redis/v9"
)

const bloomKeyPrefix = "taskflow:bloom:exec:"

type BloomFilter struct {
	filter *bloom.Filter
	ttl    time.Duration
}

func NewSharedBloomFilter(client redis.UniversalClient, ttl time.Duration) *BloomFilter {
	return &BloomFilter{filter: bloom.NewFilter(timerredis.NewUniversalClient(client), hash.NewMurmur3Encryptor(), hash.NewMurmur3AltEncryptor()), ttl: ttl}
}

func NewBloomFilter(address, password string, ttl time.Duration) *BloomFilter {
	if ttl <= 0 {
		ttl = 48 * time.Hour
	}
	provider := timerconf.NewRedisConfigProvider(&timerconf.RedisConfig{
		Network:            "tcp",
		Address:            address,
		Password:           password,
		MaxIdle:            10,
		MaxActive:          50,
		IdleTimeoutSeconds: 30,
	})
	client := timerredis.GetClient(provider)
	return &BloomFilter{
		filter: bloom.NewFilter(client, hash.NewMurmur3Encryptor(), hash.NewMurmur3AltEncryptor()),
		ttl:    ttl,
	}
}

func dayKey(day time.Time) string {
	return bloomKeyPrefix + day.UTC().Format("2006-01-02")
}

func (b *BloomFilter) MaybeSeen(ctx context.Context, fireKey string) bool {
	if b == nil || b.filter == nil {
		return false
	}
	now := time.Now().UTC()
	for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
		seen, err := b.filter.Exist(ctx, dayKey(day), fireKey)
		if err != nil {
			slog.Warn("bloom dedupe lookup failed, failing open", "fire_key", fireKey, "err", err)
			return false
		}
		if seen {
			return true
		}
	}
	return false
}

func (b *BloomFilter) MarkSeen(ctx context.Context, fireKey string) {
	if b == nil || b.filter == nil || fireKey == "" {
		return
	}
	if err := b.filter.Set(ctx, dayKey(time.Now().UTC()), fireKey, int64(b.ttl.Seconds())); err != nil {
		slog.Warn("bloom dedupe mark failed", "fire_key", fireKey, "err", err)
	}
}
