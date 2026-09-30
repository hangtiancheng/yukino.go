package limiter

import (
	"sync"
	"testing"
	"time"
)

func TestTokenBucketAllowAndRefill(t *testing.T) {
	tb := NewTokenBucket(2)
	defer tb.Stop()
	for range 2 {
		if !tb.Allow() {
			t.Fatal("expected initial tokens")
		}
	}
	if tb.Allow() {
		t.Fatal("expected bucket to be empty")
	}
	time.Sleep(1100 * time.Millisecond)
	if !tb.Allow() {
		t.Fatal("expected refill token")
	}
}

func TestTokenBucketZeroAndNegative(t *testing.T) {
	zero := NewTokenBucket(0)
	defer zero.Stop()
	if zero.Allow() {
		t.Fatal("zero-rate bucket should reject")
	}
	negative := NewTokenBucket(-1)
	defer negative.Stop()
	if negative.Allow() {
		t.Fatal("negative-rate bucket should reject")
	}
}

func TestTokenBucketConcurrentAllowAndStop(t *testing.T) {
	tb := NewTokenBucket(100)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			_ = tb.Allow()
		})
	}
	wg.Wait()
	tb.Stop()
	tb.Stop()
}
