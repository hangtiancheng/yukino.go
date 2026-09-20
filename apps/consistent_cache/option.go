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

package consistent_cache

import (
	"math/rand"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/consistent_cache/lib/log"
)

type Options struct {
	// cacheExpireSeconds is the cache TTL in seconds.
	cacheExpireSeconds int64
	// cacheExpireRandomMode adds jitter to the TTL to avoid cache stampede.
	cacheExpireRandomMode bool
	// disableExpireSeconds is the TTL of the read-path disable marker, in seconds.
	disableExpireSeconds int64
	// enableDelayMillis is the delay applied to re-enabling the read path after a write, in milliseconds.
	enableDelayMillis int64
	// randInst is the RNG used for TTL jitter. Guarded by randMu, because
	// rand.Rand is not safe for concurrent use.
	randInst *rand.Rand
	randMu   sync.Mutex
	// logger handles diagnostic output.
	logger Logger
}

func (o *Options) CacheExpireSeconds() int64 {
	if !o.cacheExpireRandomMode {
		return o.cacheExpireSeconds
	}

	o.randMu.Lock()
	defer o.randMu.Unlock()

	// Jitter between 1x and 2x the base TTL.
	return o.cacheExpireSeconds + o.randInst.Int63n(o.cacheExpireSeconds+1)
}

type Option func(*Options)

const (
	// DefaultCacheExpireSeconds is the default cache TTL (60s).
	DefaultCacheExpireSeconds = 60
	// DefaultDisableExpireSeconds is the default disable-marker TTL (10s).
	DefaultDisableExpireSeconds = 10
	// DefaultEnableDelayMillis is the default re-enable delay (1s).
	DefaultEnableDelayMillis = 1000
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
