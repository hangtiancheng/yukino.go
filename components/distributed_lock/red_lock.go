package distributed_lock

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const DefaultSingleLockTimeout = 50 * time.Millisecond

type RedLock struct {
	locks  []*RedisLock
	mu     sync.Mutex
	held   []*RedisLock
	closed bool
	RedLockOptions
}

func NewRedLock(key string, confs []*SingleNodeConf, opts ...RedLockOption) (*RedLock, error) {
	if len(confs) < 3 {
		return nil, errors.New("can not use redLock less than 3 nodes")
	}
	if key == "" {
		return nil, errors.New("redlock key is empty")
	}

	r := RedLock{}
	for _, opt := range opts {
		if opt != nil {
			opt(&r.RedLockOptions)
		}
	}

	repairRedLock(&r.RedLockOptions)
	if r.expireDuration < time.Second || r.expireDuration%time.Second != 0 {
		return nil, errors.New("redlock expiration must be a positive whole number of seconds")
	}
	if time.Duration(len(confs))*r.singleNodesTimeout*10 > r.expireDuration {
		return nil, errors.New("expire thresholds of single node is too long")
	}

	addresses := make(map[string]struct{}, len(confs))
	for _, conf := range confs {
		if conf == nil || conf.Address == "" {
			return nil, errors.New("redlock node address is empty")
		}
		addressKey := conf.Network + "://" + conf.Address
		if _, exists := addresses[addressKey]; exists {
			return nil, fmt.Errorf("duplicate redlock node: %s", addressKey)
		}
		addresses[addressKey] = struct{}{}
	}

	r.locks = make([]*RedisLock, 0, len(confs))
	for _, conf := range confs {
		client := NewClient(conf.Network, conf.Address, conf.Password, conf.Opts...)
		r.locks = append(r.locks, NewRedisLock(key, client, WithExpireSeconds(int64(r.expireDuration/time.Second))))
	}

	return &r, nil
}

type redLockAttempt struct {
	lock *RedisLock
	err  error
	cost time.Duration
}

func (r *RedLock) Lock(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("redlock is closed")
	}
	if len(r.held) > 0 {
		return errors.New("redlock is already held")
	}

	attempts := make([]redLockAttempt, len(r.locks))
	var wg sync.WaitGroup
	for i, lock := range r.locks {
		wg.Add(1)
		go func(i int, lock *RedisLock) {
			defer wg.Done()
			lockCtx, cancel := context.WithTimeout(ctx, r.singleNodesTimeout)
			defer cancel()
			startTime := time.Now()
			err := lock.lockWithWatchdog(lockCtx)
			attempts[i] = redLockAttempt{lock: lock, err: err, cost: time.Since(startTime)}
		}(i, lock)
	}
	wg.Wait()

	acquired := make([]*RedisLock, 0, len(attempts))
	timely := make([]*RedisLock, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.err != nil {
			continue
		}
		acquired = append(acquired, attempt.lock)
		if attempt.cost <= r.singleNodesTimeout {
			timely = append(timely, attempt.lock)
		}
	}

	if err := ctx.Err(); err != nil {
		failed, unlockErr := r.releaseLocks(ctx, acquired, false)
		r.held = failed
		return errors.Join(err, unlockErr)
	}

	if len(timely) < len(r.locks)/2+1 {
		failed, unlockErr := r.releaseLocks(ctx, acquired, false)
		r.held = failed
		return errors.Join(errors.New("lock failed"), unlockErr)
	}

	// Keep locks that could not be released so Unlock/Close can retry them.
	failed, _ := r.releaseLocks(ctx, difference(acquired, timely), true)
	r.held = append(timely, failed...)
	return nil
}

func (r *RedLock) Unlock(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	remaining := make([]*RedisLock, 0, len(r.held))
	var errs []error
	for _, lock := range r.held {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.singleNodesTimeout)
		err := lock.Unlock(unlockCtx)
		cancel()
		if err == nil || errors.Is(err, errLockLost) {
			continue
		}
		remaining = append(remaining, lock)
		errs = append(errs, err)
	}
	r.held = remaining
	return errors.Join(errs...)
}

func (r *RedLock) releaseLocks(ctx context.Context, locks []*RedisLock, keepWatchdog bool) ([]*RedisLock, error) {
	seen := make(map[*RedisLock]struct{}, len(locks))
	var failed []*RedisLock
	var errs []error
	for _, lock := range locks {
		if lock == nil {
			continue
		}
		if _, ok := seen[lock]; ok {
			continue
		}
		seen[lock] = struct{}{}
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.singleNodesTimeout)
		err := lock.Unlock(unlockCtx)
		cancel()
		if err == nil || errors.Is(err, errLockLost) {
			continue
		}
		failed = append(failed, lock)
		if !keepWatchdog {
			lock.stopWatchDog()
		}
		errs = append(errs, err)
	}
	return failed, errors.Join(errs...)
}

func difference(left, right []*RedisLock) []*RedisLock {
	rightSet := make(map[*RedisLock]struct{}, len(right))
	for _, lock := range right {
		rightSet[lock] = struct{}{}
	}
	result := make([]*RedisLock, 0, len(left)-len(right))
	for _, lock := range left {
		if _, ok := rightSet[lock]; !ok {
			result = append(result, lock)
		}
	}
	return result
}

func (r *RedLock) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	unlockErr := r.Unlock(ctx)
	var closeErr error
	for _, lock := range r.locks {
		client, ok := lock.client.(*Client)
		if !ok {
			continue
		}
		if err := client.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return errors.Join(unlockErr, closeErr)
}
