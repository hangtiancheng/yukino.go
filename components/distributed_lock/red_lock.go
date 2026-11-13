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
	var successCnt int
	var attempted []*RedisLock
	var acquired []*RedisLock
	var timely []*RedisLock
	for _, lock := range r.locks {
		startTime := time.Now()
		lockCtx, cancel := context.WithTimeout(ctx, r.singleNodesTimeout)
		err := lock.lockWithWatchdog(lockCtx, ctx)
		cancel()
		cost := time.Since(startTime)
		attempted = append(attempted, lock)
		if err == nil {
			acquired = append(acquired, lock)
			if cost <= r.singleNodesTimeout {
				successCnt++
				timely = append(timely, lock)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		r.unlockLocks(ctx, attempted)
		return err
	}

	if successCnt < len(r.locks)/2+1 {
		r.unlockLocks(ctx, acquired, attempted)
		return errors.New("lock failed")
	}
	r.unlockLocks(ctx, difference(attempted, timely))
	r.held = timely

	return nil
}

func (r *RedLock) Unlock(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var err error
	remaining := make([]*RedisLock, 0, len(r.held))
	for _, lock := range r.held {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.singleNodesTimeout)
		if _err := lock.Unlock(unlockCtx); _err != nil {
			remaining = append(remaining, lock)
			if err == nil {
				err = _err
			}
		}
		cancel()
	}
	r.held = remaining
	return err
}

func (r *RedLock) unlockLocks(ctx context.Context, locks ...[]*RedisLock) {
	seen := make(map[*RedisLock]struct{})
	for _, lockSet := range locks {
		for _, lock := range lockSet {
			if _, ok := seen[lock]; ok {
				continue
			}
			seen[lock] = struct{}{}
			unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.singleNodesTimeout)
			_ = lock.Unlock(unlockCtx)
			cancel()
		}
	}
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
