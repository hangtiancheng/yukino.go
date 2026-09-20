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

package redis_lock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/redis_lock/utils"
)

const RedisLockKeyPrefix = "REDIS_LOCK_PREFIX_"

var (
	ErrLockAcquiredByOthers = errors.New("lock is acquired by others")
	// errLockLost reports that the lock is no longer owned by this client.
	errLockLost = errors.New("can not expire lock without ownership of lock")
)

func IsRetryableErr(err error) bool {
	return errors.Is(err, ErrLockAcquiredByOthers)
}

// RedisLock is a redis-backed distributed lock. It is non-reentrant but symmetric.
type RedisLock struct {
	LockOptions
	key    string
	token  string
	client LockClient

	// runningDog flags whether the watchdog goroutine is running.
	runningDog atomic.Int32
	// watchDogMu guards stopDog: Lock (watchDog) writes it and Unlock reads it,
	// possibly from different goroutines.
	watchDogMu sync.Mutex
	// stopDog cancels the watchdog goroutine.
	stopDog context.CancelFunc
}

func NewRedisLock(key string, client LockClient, opts ...LockOption) *RedisLock {
	r := RedisLock{
		key:    key,
		token:  utils.GetProcessAndGoroutineIDStr(),
		client: client,
	}

	for _, opt := range opts {
		opt(&r.LockOptions)
	}

	repairLock(&r.LockOptions)
	return &r
}

// Lock acquires the lock. On success it starts the watchdog (if enabled).
func (r *RedisLock) Lock(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			return
		}
		// Start the watchdog only after a successful acquire.
		// The lock is non-reentrant, so the watchdog cannot be started twice for the same lock.
		r.watchDog(ctx)
	}()

	// Always attempt one acquire first, regardless of blocking mode.
	err = r.tryLock(ctx)
	if err == nil {
		return nil
	}

	// Non-blocking mode: surface the error immediately.
	if !r.isBlock {
		return err
	}

	// Bail out on non-retryable errors.
	if !IsRetryableErr(err) {
		return err
	}

	// Blocking mode: keep polling until acquired or timed out.
	err = r.blockingLock(ctx)
	return
}

func (r *RedisLock) tryLock(ctx context.Context) error {
	// Try to set the lock key only if it does not exist.
	reply, err := r.client.SetNEX(ctx, r.getLockKey(), r.token, r.expireSeconds)
	if err != nil {
		return err
	}
	if reply != 1 {
		return fmt.Errorf("reply: %d, err: %w", reply, ErrLockAcquiredByOthers)
	}

	return nil
}

// watchDog starts the renewal goroutine.
func (r *RedisLock) watchDog(ctx context.Context) {
	// 1. Skip when watchdog mode is disabled.
	if !r.watchDogMode {
		return
	}

	// 2. Cancel any previous watchdog and register the new cancel func atomically.
	// Waiting for the previous dog to recycle here could deadlock Lock forever if
	// a concurrent Unlock already consumed (or is about to consume) the old
	// stopDog, so the old dog is cancelled instead and exits immediately.
	r.watchDogMu.Lock()
	if r.stopDog != nil {
		r.stopDog()
	}
	dogCtx, cancel := context.WithCancel(ctx)
	r.stopDog = cancel
	r.watchDogMu.Unlock()

	// 3. Start the watchdog.
	r.runningDog.Store(1)
	go func() {
		defer func() {
			r.runningDog.Store(0)
		}()
		r.runWatchDog(dogCtx)
	}()
}

func (r *RedisLock) runWatchDog(ctx context.Context) {
	ticker := time.NewTicker(WatchDogWorkStepSeconds * time.Second)
	defer ticker.Stop()

	for {
		// React to cancellation immediately instead of waiting for the next tick.
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// The watchdog keeps renewing the lock while the caller holds it.
		// Renewal is done via a Lua script that verifies ownership before extending the TTL.
		// Add an extra 5s to the TTL so that network latency does not cause premature expiry.
		err := r.DelayExpire(ctx, WatchDogWorkStepSeconds+5)
		if err == nil {
			continue
		}

		// The lock is no longer ours: renewing is pointless, so stop the watchdog
		// instead of looping forever. Transient errors are retried on the next tick.
		if errors.Is(err, errLockLost) {
			return
		}
	}
}

// DelayExpire extends the lock TTL. Atomicity is guaranteed by a Lua script that checks ownership first.
func (r *RedisLock) DelayExpire(ctx context.Context, expireSeconds int64) error {
	keysAndArgs := []any{r.getLockKey(), r.token, expireSeconds}
	reply, err := r.client.Eval(ctx, LuaCheckAndExpireDistributionLock, 1, keysAndArgs)
	if err != nil {
		return err
	}

	if ret, _ := reply.(int64); ret != 1 {
		return errLockLost
	}

	return nil
}

func (r *RedisLock) blockingLock(ctx context.Context) error {
	// Upper bound on how long to wait for the lock.
	timeoutCh := time.After(time.Duration(r.blockWaitingSeconds) * time.Second)
	// Poll every 50 ms.
	ticker := time.NewTicker(time.Duration(50) * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		select {
		case <-ctx.Done():
			return fmt.Errorf("lock failed, ctx timeout, err: %w", ctx.Err())
		case <-timeoutCh:
			return fmt.Errorf("block waiting time out, err: %w", ErrLockAcquiredByOthers)
		default:
		}

		// Try to acquire.
		err := r.tryLock(ctx)
		if err == nil {
			return nil
		}

		// Surface non-retryable errors.
		if !IsRetryableErr(err) {
			return err
		}
	}

	// Unreachable.
	return nil
}

// Unlock releases the lock. Atomicity is guaranteed by a Lua script that checks ownership first.
func (r *RedisLock) Unlock(ctx context.Context) error {
	defer func() {
		// Stop the watchdog. stopDog may be written concurrently by watchDog
		// (a Lock on another goroutine), so read it under the mutex.
		r.watchDogMu.Lock()
		stopDog := r.stopDog
		r.watchDogMu.Unlock()
		if stopDog != nil {
			stopDog()
		}
	}()

	keysAndArgs := []any{r.getLockKey(), r.token}
	reply, err := r.client.Eval(ctx, LuaCheckAndDeleteDistributionLock, 1, keysAndArgs)
	if err != nil {
		return err
	}

	if ret, _ := reply.(int64); ret != 1 {
		return errors.New("can not unlock without ownership of lock")
	}

	return nil
}

func (r *RedisLock) getLockKey() string {
	return RedisLockKeyPrefix + r.key
}
