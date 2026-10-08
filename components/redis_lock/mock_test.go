package redis_lock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeClient is an in-memory LockClient implementing the subset of redis
// semantics the lock relies on: SET NX EX plus the token-checked DEL/EXPIRE
// Lua scripts. The clock is injectable so lease expiry can be simulated
// without waiting or contacting a real redis.
type fakeClient struct {
	mu       sync.Mutex
	values   map[string]string
	expireAt map[string]time.Time
	current  time.Time
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		values:   make(map[string]string),
		expireAt: make(map[string]time.Time),
		current:  time.Now(),
	}
}

func (f *fakeClient) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = f.current.Add(d)
}

func (f *fakeClient) ownedLocked(key, token string) bool {
	if f.values[key] != token {
		return false
	}
	exp, ok := f.expireAt[key]
	return ok && f.current.Before(exp)
}

func (f *fakeClient) SetNEX(_ context.Context, key, value string, expireSeconds int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.values[key]; exists && f.current.Before(f.expireAt[key]) {
		return 0, nil
	}
	f.values[key] = value
	f.expireAt[key] = f.current.Add(time.Duration(expireSeconds) * time.Second)
	return 1, nil
}

func (f *fakeClient) Eval(_ context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if keyCount != 1 || len(keysAndArgs) < 2 {
		return nil, fmt.Errorf("fakeClient: unexpected eval args (keyCount=%d, len=%d)", keyCount, len(keysAndArgs))
	}
	key := fmt.Sprintf("%v", keysAndArgs[0])
	token := fmt.Sprintf("%v", keysAndArgs[1])
	if !f.ownedLocked(key, token) {
		return int64(0), nil
	}

	switch src {
	case LuaCheckAndDeleteDistributionLock:
		delete(f.values, key)
		delete(f.expireAt, key)
		return int64(1), nil
	case LuaCheckAndExpireDistributionLock:
		seconds, _ := keysAndArgs[2].(int64)
		f.expireAt[key] = f.current.Add(time.Duration(seconds) * time.Second)
		return int64(1), nil
	default:
		return nil, fmt.Errorf("fakeClient: unexpected script")
	}
}

// newLockInFreshGoroutine builds the lock inside a fresh goroutine so that its
// token (process + goroutine id) differs from the caller's.
func newLockInFreshGoroutine(key string, client LockClient, opts ...LockOption) *RedisLock {
	var l *RedisLock
	done := make(chan struct{})
	go func() {
		defer close(done)
		l = NewRedisLock(key, client, opts...)
	}()
	<-done
	return l
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestMockLockUnlockAndContention(t *testing.T) {
	fc := newFakeClient()
	ctx := context.Background()

	owner := NewRedisLock("k", fc, WithExpireSeconds(30))
	other := newLockInFreshGoroutine("k", fc, WithExpireSeconds(30))

	if err := owner.Lock(ctx); err != nil {
		t.Fatalf("owner.Lock: %v", err)
	}
	otherLockErr := other.Lock(ctx)
	if !errors.Is(otherLockErr, ErrLockAcquiredByOthers) {
		t.Fatalf("other.Lock err = %v, want ErrLockAcquiredByOthers", otherLockErr)
	}
	if !IsRetryableErr(otherLockErr) {
		t.Fatal("contention error should be retryable")
	}

	// A non-owner can not unlock.
	if err := other.Unlock(ctx); err == nil {
		t.Fatal("other.Unlock should fail without ownership")
	}
	// The key is still locked after the failed unlock attempt.
	if err := other.Lock(ctx); !errors.Is(err, ErrLockAcquiredByOthers) {
		t.Fatalf("lock should still be held after failed unlock, err = %v", err)
	}
	// Owner releases; contender takes over.
	if err := owner.Unlock(ctx); err != nil {
		t.Fatalf("owner.Unlock: %v", err)
	}
	if err := other.Lock(ctx); err != nil {
		t.Fatalf("other.Lock after release: %v", err)
	}
	if err := other.Unlock(ctx); err != nil {
		t.Fatalf("other.Unlock: %v", err)
	}
	// Unlocking again without ownership fails.
	if err := owner.Unlock(ctx); err == nil {
		t.Fatal("owner.Unlock on a released lock should fail")
	}
}

func TestMockBlockingLockAcquiresAfterRelease(t *testing.T) {
	fc := newFakeClient()
	ctx := context.Background()

	holder := NewRedisLock("k", fc, WithExpireSeconds(30))
	if err := holder.Lock(ctx); err != nil {
		t.Fatalf("holder.Lock: %v", err)
	}

	waiter := newLockInFreshGoroutine("k", fc, WithBlock(), WithBlockWaitingSeconds(3))

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = holder.Unlock(ctx)
	}()

	if err := waiter.Lock(ctx); err != nil {
		t.Fatalf("waiter.Lock: %v", err)
	}
}

func TestMockBlockingLockTimesOut(t *testing.T) {
	fc := newFakeClient()
	ctx := context.Background()

	holder := NewRedisLock("k", fc, WithExpireSeconds(30))
	if err := holder.Lock(ctx); err != nil {
		t.Fatalf("holder.Lock: %v", err)
	}
	defer func() { _ = holder.Unlock(ctx) }()

	waiter := newLockInFreshGoroutine("k", fc, WithBlock(), WithBlockWaitingSeconds(1))

	start := time.Now()
	err := waiter.Lock(ctx)
	if !errors.Is(err, ErrLockAcquiredByOthers) {
		t.Fatalf("waiter.Lock err = %v, want ErrLockAcquiredByOthers", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("waiter.Lock returned too early: %v", elapsed)
	}
}

func TestMockWatchDogStartsAndStops(t *testing.T) {
	fc := newFakeClient()
	ctx := context.Background()

	l := NewRedisLock("k", fc) // watchdog mode: no explicit TTL
	if err := l.Lock(ctx); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if l.runningDog.Load() != 1 {
		t.Fatal("watchdog should be running after Lock")
	}
	if err := l.Unlock(ctx); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	// The watchdog must exit promptly on unlock instead of waiting for its
	// next tick (which would take up to WatchDogWorkStepSeconds).
	waitFor(t, 2*time.Second, func() bool { return l.runningDog.Load() == 0 })
}

func TestMockDelayExpireOwnership(t *testing.T) {
	fc := newFakeClient()
	ctx := context.Background()

	l1 := NewRedisLock("k", fc, WithExpireSeconds(30))
	l2 := newLockInFreshGoroutine("k", fc, WithExpireSeconds(30))

	if err := l1.Lock(ctx); err != nil {
		t.Fatalf("l1.Lock: %v", err)
	}
	if err := l1.DelayExpire(ctx, 30); err != nil {
		t.Fatalf("owner DelayExpire: %v", err)
	}
	if err := l2.DelayExpire(ctx, 30); !errors.Is(err, errLockLost) {
		t.Fatalf("non-owner DelayExpire err = %v, want errLockLost", err)
	}
	if err := l1.Unlock(ctx); err != nil {
		t.Fatalf("l1.Unlock: %v", err)
	}
	if err := l1.DelayExpire(ctx, 30); !errors.Is(err, errLockLost) {
		t.Fatalf("DelayExpire after release err = %v, want errLockLost", err)
	}
}

func TestMockExpiredLeaseIsReacquirable(t *testing.T) {
	fc := newFakeClient()
	ctx := context.Background()

	l1 := NewRedisLock("k", fc, WithExpireSeconds(1))
	l2 := newLockInFreshGoroutine("k", fc, WithExpireSeconds(1))

	if err := l1.Lock(ctx); err != nil {
		t.Fatalf("l1.Lock: %v", err)
	}
	fc.advance(2 * time.Second) // the lease elapses
	if err := l1.DelayExpire(ctx, 1); !errors.Is(err, errLockLost) {
		t.Fatalf("DelayExpire after expiry err = %v, want errLockLost", err)
	}
	if err := l2.Lock(ctx); err != nil {
		t.Fatalf("expired lock should be acquirable, err = %v", err)
	}
	if err := l1.Unlock(ctx); err == nil {
		t.Fatal("l1.Unlock after losing ownership should fail")
	}
	if err := l2.Unlock(ctx); err != nil {
		t.Fatalf("l2.Unlock: %v", err)
	}
}

// TestMockConcurrentLockUnlock exercises Lock/Unlock/watchdog start-stop from
// different goroutines on a single lock object. Run with -race.
func TestMockConcurrentLockUnlock(t *testing.T) {
	fc := newFakeClient()
	ctx := context.Background()

	l := newLockInFreshGoroutine("k", fc) // watchdog mode

	// Sequential handoff: Lock on the test goroutine, Unlock on another one.
	for i := 0; i < 50; i++ {
		if err := l.Lock(ctx); err != nil {
			t.Fatalf("Lock: %v", err)
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := l.Unlock(ctx); err != nil {
				t.Errorf("Unlock: %v", err)
			}
		}()
		<-done
	}

	// Fully concurrent burst; outcomes are not asserted, only race-freedom.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = l.Lock(ctx)
		}()
		go func() {
			defer wg.Done()
			_ = l.Unlock(ctx)
		}()
	}
	wg.Wait()

	// The last successful Lock may have left a watchdog running; stop it.
	_ = l.Unlock(ctx)

	waitFor(t, 2*time.Second, func() bool { return l.runningDog.Load() == 0 })
}

func TestLockOptionDefaults(t *testing.T) {
	l := NewRedisLock("k", nil)
	if l.expireSeconds != DefaultLockExpireSeconds || !l.watchDogMode {
		t.Fatalf("defaults: expireSeconds=%d watchDogMode=%v", l.expireSeconds, l.watchDogMode)
	}

	l = NewRedisLock("k", nil, WithBlock())
	if l.blockWaitingSeconds != 5 {
		t.Fatalf("default blockWaitingSeconds = %d, want 5", l.blockWaitingSeconds)
	}

	l = NewRedisLock("k", nil, WithExpireSeconds(10))
	if l.watchDogMode {
		t.Fatal("explicit TTL should disable the watchdog")
	}

	// A non-positive TTL falls back to the watchdog defaults.
	l = NewRedisLock("k", nil, WithExpireSeconds(-1))
	if l.expireSeconds != DefaultLockExpireSeconds || !l.watchDogMode {
		t.Fatalf("negative TTL repair: expireSeconds=%d watchDogMode=%v", l.expireSeconds, l.watchDogMode)
	}
}

func TestClientEvalRejectsBadKeyCount(t *testing.T) {
	c := &Client{}
	if _, err := c.Eval(context.Background(), "return 1", 3, []any{"a", "b"}); err == nil {
		t.Fatal("Eval with keyCount > len(keysAndArgs) should fail")
	}
	if _, err := c.Eval(context.Background(), "return 1", -1, []any{"a"}); err == nil {
		t.Fatal("Eval with negative keyCount should fail")
	}
}

func TestClientSetNEXRejectsNonPositiveTTL(t *testing.T) {
	c := &Client{}
	if _, err := c.SetNEX(context.Background(), "k", "v", 0); err == nil {
		t.Fatal("SetNEX with zero TTL should fail")
	}
	if _, err := c.SetNEX(context.Background(), "k", "v", -1); err == nil {
		t.Fatal("SetNEX with negative TTL should fail")
	}
}

func TestRedLockValidation(t *testing.T) {
	confs := []*SingleNodeConf{
		{Network: "tcp", Address: "127.0.0.1:1"},
		{Network: "tcp", Address: "127.0.0.1:2"},
		{Network: "tcp", Address: "127.0.0.1:3"},
	}
	if _, err := NewRedLock("k", confs[:2]); err == nil {
		t.Fatal("NewRedLock with fewer than 3 nodes should fail")
	}
	// The per-node timeout budget (3 * 1s * 10 = 30s) exceeds the TTL (10s).
	if _, err := NewRedLock("k", confs,
		WithRedLockExpireDuration(10*time.Second),
		WithSingleNodesTimeout(time.Second)); err == nil {
		t.Fatal("NewRedLock with an oversized node-timeout budget should fail")
	}
	if _, err := NewRedLock("k", confs,
		WithRedLockExpireDuration(30*time.Second),
		WithSingleNodesTimeout(50*time.Millisecond)); err != nil {
		t.Fatalf("valid NewRedLock: %v", err)
	}
}

// TestRedLockLockFailsWithUnreachableNodes checks the failure path (including
// the release of any partially acquired nodes). All addresses point at closed
// loopback ports, so nothing external is contacted.
func TestRedLockLockFailsWithUnreachableNodes(t *testing.T) {
	confs := make([]*SingleNodeConf, 0, 3)
	for i := 0; i < 3; i++ {
		confs = append(confs, &SingleNodeConf{Network: "tcp", Address: "127.0.0.1:1"})
	}
	rl, err := NewRedLock("k", confs,
		WithRedLockExpireDuration(30*time.Second),
		WithSingleNodesTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewRedLock: %v", err)
	}
	if err := rl.Lock(context.Background()); err == nil {
		t.Fatal("RedLock.Lock should fail when no node is reachable")
	}
}
