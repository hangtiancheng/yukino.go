package limiter

import (
	"sync"
	"time"
)

type TokenBucket struct {
	tokens int
	rate   int
	mu     sync.Mutex
	stop   chan struct{}
	once   sync.Once
}

func NewTokenBucket(rate int) *TokenBucket {
	if rate < 0 {
		rate = 0
	}
	tb := &TokenBucket{tokens: rate, rate: rate, stop: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				tb.mu.Lock()
				tb.tokens = tb.rate
				tb.mu.Unlock()
			case <-tb.stop:
				return
			}
		}
	}()
	return tb
}

func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	if tb.tokens > 0 {
		tb.tokens--
		return true
	}
	return false
}

func (tb *TokenBucket) Stop() {
	tb.once.Do(func() {
		close(tb.stop)
	})
}
