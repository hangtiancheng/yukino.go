package yukino_cache

import "time"

type Value interface {
	Len() int
}

type Store interface {
	Get(key string) (Value, bool)
	Set(key string, value Value) error
	SetWithExpiration(key string, value Value, expiration time.Duration) error
	Delete(key string) bool
	Clear()
	Len() int
	Walk(fn func(Entry) bool)
	Close()
}

type StoreOptions struct {
	MaxBytes        int64
	BucketCount     uint16
	CapPerBucket    uint16
	Level2Cap       uint16
	CleanupInterval time.Duration
	OnEvicted       func(key string, value Value)
}

func NewStoreOptions() StoreOptions {
	return StoreOptions{
		MaxBytes:        8 * 1024 * 1024,
		BucketCount:     16,
		CapPerBucket:    512,
		Level2Cap:       256,
		CleanupInterval: time.Minute,
		OnEvicted:       nil,
	}
}
