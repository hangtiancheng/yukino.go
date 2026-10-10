package consistent_cache

import (
	"math/rand"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/components/consistent_cache/lib/log"
)

type Options struct {
	cacheExpireSeconds    int64
	cacheExpireRandomMode bool
	disableExpireSeconds  int64
	enableDelayMillis     int64
	randInst              *rand.Rand
	randMu                sync.Mutex
	logger                Logger
}

func (o *Options) CacheExpireSeconds() int64 {
	if !o.cacheExpireRandomMode {
		return o.cacheExpireSeconds
	}

	o.randMu.Lock()
	defer o.randMu.Unlock()

	return o.cacheExpireSeconds + o.randInst.Int63n(o.cacheExpireSeconds+1)
}

type Option func(*Options)

const (
	DefaultCacheExpireSeconds   = 60
	DefaultDisableExpireSeconds = 10
	DefaultEnableDelayMillis    = 1000
)

func WithCacheExpireSeconds(cacheExpireSeconds int64) Option {
	return func(o *Options) {
		o.cacheExpireSeconds = cacheExpireSeconds
	}
}

func WithCacheExpireRandomMode() Option {
	return func(o *Options) {
		o.cacheExpireRandomMode = true
		o.randInst = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
}

func WithDisableExpireSeconds(disableExpireSeconds int64) Option {
	return func(o *Options) {
		o.disableExpireSeconds = disableExpireSeconds
	}
}

func WithEnableDelayMillis(enableDelayMillis int64) Option {
	return func(o *Options) {
		o.enableDelayMillis = enableDelayMillis
	}
}

func WithLogger(logger Logger) Option {
	return func(o *Options) {
		o.logger = logger
	}
}

func repair(o *Options) {
	if o.cacheExpireSeconds <= 0 {
		o.cacheExpireSeconds = DefaultCacheExpireSeconds
	}

	if o.disableExpireSeconds <= 0 {
		o.disableExpireSeconds = DefaultDisableExpireSeconds
	}

	if o.enableDelayMillis <= 0 {
		o.enableDelayMillis = DefaultEnableDelayMillis
	}

	if o.logger == nil {
		o.logger = log.GetLogger()
	}
}
