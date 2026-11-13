package distributed_lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const RedisLockKeyPrefix = "distributed_lock_PREFIX_"

var (
	ErrLockAcquiredByOthers = errors.New("lock is acquired by others")
	errLockLost             = errors.New("can not expire lock without ownership of lock")
)

func IsRetryableErr(err error) bool {
	return errors.Is(err, ErrLockAcquiredByOthers)
}

type RedisLock struct {
	LockOptions
	key         string
	token       string
	client      LockClient
	operationMu sync.Mutex
	held        bool

	runningDog atomic.Int32
	watchDogMu sync.Mutex
	stopDog    context.CancelFunc
}

func NewRedisLock(key string, client LockClient, opts ...LockOption) *RedisLock {
	r := RedisLock{
		key:    key,
		client: client,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&r.LockOptions)
		}
	}

	repairLock(&r.LockOptions)
	return &r
}

func (r *RedisLock) Lock(ctx context.Context) (err error) {
	return r.lockWithWatchdog(ctx, ctx)
}

func (r *RedisLock) lockWithWatchdog(ctx, watchdogCtx context.Context) (err error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	if r.client == nil {
		return errors.New("redis lock client is nil")
	}
	if r.held {
		return errors.New("redis lock is already held by this instance")
	}
	if r.key == "" {
		return errors.New("redis lock key is empty")
	}
	if ctx == nil || watchdogCtx == nil {
		return errors.New("redis lock contexts are nil")
	}
	tokenBytes := make([]byte, 24)
	if _, err = rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("generate redis lock token: %w", err)
	}
	r.token = hex.EncodeToString(tokenBytes)
	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_, _ = r.client.Eval(cleanupCtx, LuaCheckAndDeleteDistributionLock, 1, []any{r.getLockKey(), r.token})
			return
		}
		r.held = true
		r.watchDog(watchdogCtx)
	}()

	err = r.tryLock(ctx)
	if err == nil {
		return nil
	}

	if !r.isBlock {
		return err
	}

	if !IsRetryableErr(err) {
		return err
	}

	err = r.blockingLock(ctx)
	return
}

func (r *RedisLock) tryLock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	reply, err := r.client.SetNEX(ctx, r.getLockKey(), r.token, r.expireSeconds)
	if err != nil {
		return err
	}
	if reply != 1 {
		return fmt.Errorf("reply: %d, err: %w", reply, ErrLockAcquiredByOthers)
	}

	return nil
}

func (r *RedisLock) watchDog(ctx context.Context) {
	if !r.watchDogMode {
		return
	}

	r.watchDogMu.Lock()
	if r.stopDog != nil {
		r.stopDog()
	}
	dogCtx, cancel := context.WithCancel(ctx)
	r.stopDog = cancel
	r.watchDogMu.Unlock()

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
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		err := r.DelayExpire(ctx, WatchDogWorkStepSeconds+5)
		if err == nil {
			continue
		}

		if errors.Is(err, errLockLost) {
			return
		}
	}
}

func (r *RedisLock) DelayExpire(ctx context.Context, expireSeconds int64) error {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	if r.client == nil || ctx == nil {
		return errors.New("redis lock client and context are required")
	}
	if !r.held {
		return errLockLost
	}
	if expireSeconds <= 0 {
		return errors.New("lock expiration must be positive")
	}
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
	timeoutCh := time.After(time.Duration(r.blockWaitingSeconds) * time.Second)
	ticker := time.NewTicker(time.Duration(50) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("lock failed, ctx timeout, err: %w", ctx.Err())
		case <-timeoutCh:
			return fmt.Errorf("block waiting time out, err: %w", ErrLockAcquiredByOthers)
		case <-ticker.C:
		}

		err := r.tryLock(ctx)
		if err == nil {
			return nil
		}

		if !IsRetryableErr(err) {
			return err
		}
	}
}

func (r *RedisLock) Unlock(ctx context.Context) error {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	if r.client == nil {
		return errors.New("redis lock client is nil")
	}
	if r.key == "" {
		return errors.New("redis lock key is empty")
	}
	if ctx == nil {
		return errors.New("redis lock context is nil")
	}
	keysAndArgs := []any{r.getLockKey(), r.token}
	reply, err := r.client.Eval(ctx, LuaCheckAndDeleteDistributionLock, 1, keysAndArgs)
	if err != nil {
		return err
	}

	if ret, _ := reply.(int64); ret != 1 {
		r.held = false
		r.stopWatchDog()
		return errors.New("can not unlock without ownership of lock")
	}

	r.held = false
	r.stopWatchDog()
	return nil
}

func (r *RedisLock) stopWatchDog() {
	r.watchDogMu.Lock()
	stopDog := r.stopDog
	r.stopDog = nil
	r.watchDogMu.Unlock()
	if stopDog != nil {
		stopDog()
	}
}

func (r *RedisLock) getLockKey() string {
	return RedisLockKeyPrefix + r.key
}
