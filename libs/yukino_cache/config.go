package yukino_cache

import "hash/crc32"

type ConHashConfig struct {
	DefaultReplicas int
	HashFunc        func(data []byte) uint32

	MinReplicas          int
	MaxReplicas          int
	LoadBalanceThreshold float64
}

var DefaultConHashConfig = &ConHashConfig{
	DefaultReplicas:      50,
	MinReplicas:          10,
	MaxReplicas:          200,
	HashFunc:             crc32.ChecksumIEEE,
	LoadBalanceThreshold: 0.25,
}
