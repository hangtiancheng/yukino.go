package redis_lock

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type hookedClient struct {
	LockClient
	set  func(context.Context, string, string, int64) (int64, error)
	eval func(context.Context, string, int, []any) (any, error)
}

func (c *hookedClient) SetNEX(ctx context.Context, key, token string, ttl int64) (int64, error) {
	if c.set != nil {
		return c.set(ctx, key, token, ttl)
	}
	return c.LockClient.SetNEX(ctx, key, token, ttl)
}
func (c *hookedClient) Eval(ctx context.Context, script string, n int, args []any) (any, error) {
	if c.eval != nil {
		return c.eval(ctx, script, n, args)
	}
	return c.LockClient.Eval(ctx, script, n, args)
}
func TestOwnershipLossClearsLocalLease(t *testing.T) {
	fc := newFakeClient()
	lock := NewRedisLock("k", fc, WithExpireSeconds(1))
	ctx := context.Background()
	if err := lock.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	fc.advance(2 * time.Second)
	if err := lock.DelayExpire(ctx, 1); !errors.Is(err, ErrLockLost) {
		t.Fatal(err)
	}
	if lock.Held() {
		t.Fatal("lost lock remains held")
	}
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("reacquire after confirmed loss: %v", err)
	}
	if err := lock.Unlock(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestCancelledSuccessfulSetIsCleaned(t *testing.T) {
	fc := newFakeClient()
	ctx, cancel := context.WithCancel(context.Background())
	client := &hookedClient{LockClient: fc}
	client.set = func(c context.Context, k, v string, ttl int64) (int64, error) {
		reply, err := fc.SetNEX(c, k, v, ttl)
		cancel()
		return reply, err
	}
	lock := NewRedisLock("k", client, WithExpireSeconds(1))
	if err := lock.Lock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if len(fc.values) != 0 {
		t.Fatal("cancelled acquisition leaked a key")
	}
}
func TestFailedAcquisitionCleanupCanBeRetried(t *testing.T) {
	fc := newFakeClient()
	client := &hookedClient{LockClient: fc}
	fail := true
	client.set = func(c context.Context, k, v string, ttl int64) (int64, error) {
		_, _ = fc.SetNEX(c, k, v, ttl)
		return 0, errors.New("lost SET reply")
	}
	client.eval = func(c context.Context, s string, n int, a []any) (any, error) {
		if fail {
			return nil, errors.New("cleanup failed")
		}
		return fc.Eval(c, s, n, a)
	}
	lock := NewRedisLock("k", client, WithExpireSeconds(1))
	if err := lock.Lock(context.Background()); err == nil {
		t.Fatal("expected SET error")
	}
	fail = false
	if err := lock.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.values) != 0 {
		t.Fatal("cleanup token was discarded")
	}
}
func TestFailedUnlockStopsWatchdog(t *testing.T) {
	fc := newFakeClient()
	client := &hookedClient{LockClient: fc}
	lock := NewRedisLock("k", client)
	if err := lock.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.eval = func(context.Context, string, int, []any) (any, error) { return nil, errors.New("network error") }
	if err := lock.Unlock(context.Background()); err == nil {
		t.Fatal("expected transport error")
	}
	if lock.Held() {
		t.Fatal("lease must be inactive after failed unlock")
	}
	waitFor(t, time.Second, func() bool { return lock.runningDog.Load() == 0 })
	client.eval = nil
	if err := lock.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestWatchdogAdoptsManualRenewalTTL(t *testing.T) {
	fc := newFakeClient()
	client := &hookedClient{LockClient: fc}
	var renewals atomic.Int32
	client.eval = func(c context.Context, s string, n int, a []any) (any, error) {
		if s == LuaCheckAndExpireDistributionLock {
			renewals.Add(1)
		}
		return fc.Eval(c, s, n, a)
	}
	lock := NewRedisLock("k", client)
	ctx := context.Background()
	if err := lock.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock(ctx)
	if err := lock.DelayExpire(ctx, 1); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool { return renewals.Load() >= 2 })
	if !lock.Held() {
		t.Fatal("renewal did not keep lease active")
	}
}
func TestRedLockSharesTokenAndRenewsQuorum(t *testing.T) {
	a, b, c := newFakeClient(), newFakeClient(), newFakeClient()
	ctx := context.Background()
	lock, err := NewRedLockWithClients("k", []LockClient{a, b, c}, WithRedLockExpireDuration(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err = lock.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if a.values[lock.key] != b.values[lock.key] || a.values[lock.key] != c.values[lock.key] {
		t.Fatal("masters received different tokens")
	}
	if !lock.Held() || lock.Remaining() <= 0 {
		t.Fatal("missing local validity")
	}
	if err = lock.DelayExpire(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if err = lock.Unlock(ctx); err != nil {
		t.Fatal(err)
	}
	if len(a.values)+len(b.values)+len(c.values) != 0 {
		t.Fatal("release leaked keys")
	}
}
func TestRedLockRejectsExpiredOverallValidity(t *testing.T) {
	a, b, c := newFakeClient(), newFakeClient(), newFakeClient()
	slow := &hookedClient{LockClient: c}
	slow.set = func(ctx context.Context, k, v string, ttl int64) (int64, error) {
		reply, err := c.SetNEX(ctx, k, v, ttl)
		time.Sleep(1050 * time.Millisecond)
		return reply, err
	}
	lock, err := NewRedLockWithClients("k", []LockClient{a, b, slow}, WithRedLockExpireDuration(time.Second), WithSingleNodesTimeout(20*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if err = lock.Lock(context.Background()); err == nil {
		t.Fatal("quorum acknowledged after the overall lease expired")
	}
	if lock.Held() {
		t.Fatal("expired quorum marked held")
	}
	if len(a.values)+len(b.values)+len(c.values) != 0 {
		t.Fatal("failed attempt was not cleaned up")
	}
}
func TestRedLockCleansNodesWithLostSetReplies(t *testing.T) {
	a, b, c := newFakeClient(), newFakeClient(), newFakeClient()
	lost := func(fc *fakeClient) *hookedClient {
		return &hookedClient{LockClient: fc, set: func(ctx context.Context, k, v string, ttl int64) (int64, error) {
			_, _ = fc.SetNEX(ctx, k, v, ttl)
			return 0, errors.New("lost reply")
		}}
	}
	lock, err := NewRedLockWithClients("k", []LockClient{lost(a), lost(b), c})
	if err != nil {
		t.Fatal(err)
	}
	if err = lock.Lock(context.Background()); err == nil {
		t.Fatal("missing quorum error")
	}
	if len(a.values)+len(b.values)+len(c.values) != 0 {
		t.Fatal("uncertain SET left locks behind")
	}
}
func TestRedLockRejectsDuplicateClients(t *testing.T) {
	a, b := newFakeClient(), newFakeClient()
	if _, err := NewRedLockWithClients("k", []LockClient{a, b, a}); err == nil {
		t.Fatal("duplicate clients accepted")
	}
	var nilClient *fakeClient
	if _, err := NewRedLockWithClients("k", []LockClient{a, b, nilClient}); err == nil {
		t.Fatal("typed nil accepted")
	}
}

func TestBlockBudgetBoundsInitialAttempt(t *testing.T) {
	fc := newFakeClient()
	client := &hookedClient{LockClient: fc}
	client.set = func(ctx context.Context, _ string, _ string, _ int64) (int64, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 1100*time.Millisecond {
			t.Error("initial SET did not inherit blocking wait budget")
		}
		<-ctx.Done()
		return 0, ctx.Err()
	}
	lock := NewRedisLock("k", client, WithBlock(), WithBlockWaitingSeconds(1))
	if err := lock.Lock(context.Background()); !errors.Is(err, ErrLockAcquiredByOthers) {
		t.Fatal(err)
	}
}
func TestRenewalTransportErrorFailsClosed(t *testing.T) {
	fc := newFakeClient()
	client := &hookedClient{LockClient: fc}
	lock := NewRedisLock("k", client)
	if err := lock.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.eval = func(context.Context, string, int, []any) (any, error) { return nil, errors.New("network") }
	if err := lock.DelayExpire(context.Background(), 1); err == nil {
		t.Fatal("expected error")
	}
	if lock.Held() {
		t.Fatal("lease remained active after uncertain renewal")
	}
	waitFor(t, time.Second, func() bool { return lock.runningDog.Load() == 0 })
	client.eval = nil
	if err := lock.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
}
