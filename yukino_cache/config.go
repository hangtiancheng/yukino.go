package yukino_cache

import "hash/crc32"

// Config controls consistent hash ring behavior.
type ConHashConfig struct {
	DefaultReplicas int
	HashFunc        func(data []byte) uint32

	// Deprecated: the auto-rebalancer was removed to keep the key-to-node
	// mapping stable (groupcache semantics). These fields are ignored.
	MinReplicas          int
	MaxReplicas          int
	LoadBalanceThreshold float64
}

// DefaultConHashConfig is the default consistent hash configuration.
var DefaultConHashConfig = &ConHashConfig{
	DefaultReplicas:      50,
	MinReplicas:          10,
	MaxReplicas:          200,
	HashFunc:             crc32.ChecksumIEEE,
	LoadBalanceThreshold: 0.25,
}
