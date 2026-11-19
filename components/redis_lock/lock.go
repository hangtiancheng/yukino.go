package redis_lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

const RedisLockKeyPrefix = "DISTRIBUTED_LOCK_PREFIX_"
const commandTimeout = 2 * time.Second

var (
	ErrLockAcquiredByOthers = errors.New("lock is acquired by others")
	ErrLockLost             = errors.New("lock ownership or lease validity was lost")
	ErrAlreadyHeld          = errors.New("redis lock instance already has a lease")
	errLockLost             = ErrLockLost
)

func IsRetryableErr(err error) bool { return errors.Is(err, ErrLockAcquiredByOthers) }

// RedisLock is non-reentrant and must not be copied. A context controls only
// acquisition; the watchdog lives until Unlock or lease loss. Held is a local
// conservative estimate, not a Redis ownership query.
type RedisLock struct {
	LockOptions
	key         string
	token       string
	client      LockClient
	operationMu sync.Mutex
	held        bool
	validUntil  time.Time
	leaseTTL    time.Duration
	renewed     chan struct{}
	runningDog  atomic.Int32
	watchDogMu  sync.Mutex
	stopDog     context.CancelFunc
}

func NewRedisLock(key string, client LockClient, opts ...LockOption) *RedisLock {
	r := &RedisLock{key: key, client: client}
	for _, opt := range opts {
		if opt != nil {
			opt(&r.LockOptions)
		}
	}
	repairLock(&r.LockOptions)
	return r
}
func newToken() (string, error) {
	var bytes [24]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate redis lock token: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
func leaseDrift(ttl time.Duration) time.Duration    { return ttl/100 + 2*time.Millisecond }
func (r *RedisLock) Lock(ctx context.Context) error { return r.lockWithWatchdog(ctx) }
func (r *RedisLock) lockWithWatchdog(ctx context.Context) error {
	token, err := newToken()
	if err != nil {
		return err
	}
	return r.lockWithToken(ctx, token)
}
func (r *RedisLock) lockWithToken(ctx context.Context, token string) (err error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	if r.client == nil || ctx == nil || r.key == "" {
		return errors.New("redis lock requires client, context and key")
	}
	if r.expireSeconds <= 0 || r.expireSeconds > math.MaxInt64/int64(time.Second) {
		return errors.New("lock expiration exceeds time.Duration")
	}
	if r.token != "" && time.Now().Before(r.validUntil) {
		return ErrAlreadyHeld
	}
	r.stopWatchDog()
	r.held = false
	r.token = token
	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commandTimeout)
			defer cancel()
			_, cleanupErr := r.client.Eval(cleanupCtx, LuaCheckAndDeleteDistributionLock, 1, []any{r.getLockKey(), token})
			if cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("lock cleanup: %w", cleanupErr))
			}
			if cleanupErr == nil {
				r.token = ""
			} else {
				r.validUntil = time.Now().Add(time.Duration(r.expireSeconds) * time.Second)
			}
			return
		}
		r.held = true
		r.watchDog(ctx)
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	attemptCtx := ctx
	if r.isBlock {
		if r.blockWaitingSeconds <= 0 || r.blockWaitingSeconds > math.MaxInt64/int64(time.Second) {
			return errors.New("blocking wait must fit a positive time.Duration")
		}
		var cancel context.CancelFunc
		attemptCtx, cancel = context.WithTimeout(ctx, time.Duration(r.blockWaitingSeconds)*time.Second)
		defer cancel()
	}
	err = r.tryLock(attemptCtx)
	if err != nil && r.isBlock && IsRetryableErr(err) {
		err = r.blockingLock(attemptCtx)
	}
	if err != nil && ctx.Err() == nil && attemptCtx.Err() != nil {
		return fmt.Errorf("block waiting timed out: %w", ErrLockAcquiredByOthers)
	}
	return err

}
func (r *RedisLock) tryLock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	started := time.Now()
	reply, err := r.client.SetNEX(commandCtx, r.getLockKey(), r.token, r.expireSeconds)
	if err != nil {
		return err
	}
	if reply == 0 {
		return ErrLockAcquiredByOthers
	}
	if reply != 1 {
		return fmt.Errorf("unexpected redis SET reply: %d", reply)
	}
	ttl := time.Duration(r.expireSeconds) * time.Second
	r.validUntil = started.Add(ttl - leaseDrift(ttl))
	r.leaseTTL = ttl
	if err := commandCtx.Err(); err != nil {
		return err
	}
	if !time.Now().Before(r.validUntil) {
		return ErrLockLost
	}
	return nil
}
func (r *RedisLock) blockingLock(ctx context.Context) error {
	if r.blockWaitingSeconds > math.MaxInt64/int64(time.Second) {
		return errors.New("blocking wait exceeds time.Duration")
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(r.blockWaitingSeconds)*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("block waiting timed out: %w", ErrLockAcquiredByOthers)
		case <-ticker.C:
		}
		err := r.tryLock(waitCtx)
		if err == nil {
			return nil
		}
		if waitCtx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("block waiting timed out: %w", ErrLockAcquiredByOthers)
		}
		if !IsRetryableErr(err) {
			return err
		}
	}
}
func (r *RedisLock) watchDog(ctx context.Context) {
	if !r.watchDogMode {
		return
	}
	dogCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.watchDogMu.Lock()
	r.stopDog = cancel
	r.watchDogMu.Unlock()
	token := r.token
	r.renewed = make(chan struct{}, 1)
	wake := r.renewed
	ttl := r.leaseTTL
	r.runningDog.Add(1)
	go func() { defer r.runningDog.Add(-1); r.runWatchDog(dogCtx, token, wake, ttl) }()
}
func (r *RedisLock) runWatchDog(ctx context.Context, token string, wake <-chan struct{}, ttl time.Duration) {
	timer := time.NewTimer(ttl / 3)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			r.operationMu.Lock()
			if ctx.Err() != nil || token != r.token || !r.held {
				r.operationMu.Unlock()
				return
			}
			ttl = r.leaseTTL
			r.operationMu.Unlock()
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(ttl / 3)
			continue
		case <-timer.C:
		}
		r.operationMu.Lock()
		if ctx.Err() != nil || token != r.token {
			r.operationMu.Unlock()
			return
		}
		seconds := int64(r.leaseTTL / time.Second)
		err := r.delayExpire(ctx, seconds)
		ttl = r.leaseTTL
		r.operationMu.Unlock()
		if err != nil {
			return
		}
		timer.Reset(ttl / 3)
	}
}
func (r *RedisLock) DelayExpire(ctx context.Context, expireSeconds int64) error {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	return r.delayExpire(ctx, expireSeconds)
}
func (r *RedisLock) delayExpire(ctx context.Context, seconds int64) error {
	if r.client == nil || ctx == nil {
		return errors.New("redis lock requires client and context")
	}
	if seconds <= 0 || seconds > math.MaxInt64/int64(time.Second) {
		return errors.New("lock expiration must fit a positive time.Duration")
	}
	if !r.held || !time.Now().Before(r.validUntil) {
		r.held = false
		r.stopWatchDog()
		return ErrLockLost
	}
	started, previous := time.Now(), r.validUntil
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	reply, err := r.client.Eval(commandCtx, LuaCheckAndExpireDistributionLock, 1, []any{r.getLockKey(), r.token, seconds})
	if err != nil {
		r.held = false
		r.stopWatchDog()
		return err
	}
	ret, ok := reply.(int64)
	if !ok {
		r.held = false
		r.stopWatchDog()
		return fmt.Errorf("unexpected redis renewal reply: %T", reply)
	}
	ttl := time.Duration(seconds) * time.Second
	until := started.Add(ttl - leaseDrift(ttl))
	if ret == 0 {
		r.held = false
		r.stopWatchDog()
		r.token = ""
		r.validUntil = time.Time{}
		return ErrLockLost
	}
	if ret != 1 || commandCtx.Err() != nil || !time.Now().Before(previous) || !time.Now().Before(until) {
		r.held = false
		r.stopWatchDog()
		r.validUntil = until
		return ErrLockLost
	}
	r.validUntil = until
	r.leaseTTL = ttl
	if r.renewed != nil {
		select {
		case r.renewed <- struct{}{}:
		default:
		}
	}
	return nil
}
func (r *RedisLock) Unlock(ctx context.Context) error {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	if r.client == nil || ctx == nil || r.key == "" {
		return errors.New("redis lock requires client, context and key")
	}
	r.stopWatchDog()
	r.held = false
	if r.token == "" {
		return ErrLockLost
	}
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	reply, err := r.client.Eval(commandCtx, LuaCheckAndDeleteDistributionLock, 1, []any{r.getLockKey(), r.token})
	if err != nil {
		return err
	}
	ret, ok := reply.(int64)
	if !ok || (ret != 0 && ret != 1) {
		return fmt.Errorf("unexpected redis release reply: %v", reply)
	}
	r.token = ""
	r.validUntil = time.Time{}
	if ret != 1 {
		return ErrLockLost
	}
	return nil
}
func (r *RedisLock) Held() bool {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	return r.held && time.Now().Before(r.validUntil)
}
func (r *RedisLock) Remaining() time.Duration {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	if !r.held {
		return 0
	}
	return max(0, time.Until(r.validUntil))
}
func (r *RedisLock) stopWatchDog() {
	r.watchDogMu.Lock()
	cancel := r.stopDog
	r.stopDog = nil
	r.watchDogMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (r *RedisLock) getLockKey() string { return RedisLockKeyPrefix + r.key }
