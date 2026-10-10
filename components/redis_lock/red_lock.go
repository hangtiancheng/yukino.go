package redis_lock

import (
	"context"
	"errors"
	"time"
)

const DefaultSingleLockTimeout = 50 * time.Millisecond

type RedLock struct {
	locks []*RedisLock
	RedLockOptions
}

func NewRedLock(key string, confs []*SingleNodeConf, opts ...RedLockOption) (*RedLock, error) {
	if len(confs) < 3 {
		return nil, errors.New("can not use redLock less than 3 nodes")
	}

	r := RedLock{}
	for _, opt := range opts {
		opt(&r.RedLockOptions)
	}

	repairRedLock(&r.RedLockOptions)
	if r.expireDuration > 0 && time.Duration(len(confs))*r.singleNodesTimeout*10 > r.expireDuration {
		return nil, errors.New("expire thresholds of single node is too long")
	}

	r.locks = make([]*RedisLock, 0, len(confs))
	for _, conf := range confs {
		client := NewClient(conf.Network, conf.Address, conf.Password, conf.Opts...)
		r.locks = append(r.locks, NewRedisLock(key, client, WithExpireSeconds(int64(r.expireDuration.Seconds()))))
	}

	return &r, nil
}

func (r *RedLock) Lock(ctx context.Context) error {
	var successCnt int
	var acquired []*RedisLock
	for _, lock := range r.locks {
		startTime := time.Now()
		err := lock.Lock(ctx)
		cost := time.Since(startTime)
		if err == nil && cost <= r.singleNodesTimeout {
			successCnt++
			acquired = append(acquired, lock)
		}
	}

	if successCnt < len(r.locks)>>1+1 {
		unlockCtx := context.WithoutCancel(ctx)
		for _, lock := range acquired {
			_ = lock.Unlock(unlockCtx)
		}
		return errors.New("lock failed")
	}

	return nil
}

func (r *RedLock) Unlock(ctx context.Context) error {
	var err error
	for _, lock := range r.locks {
		if _err := lock.Unlock(ctx); _err != nil {
			if err == nil {
				err = _err
			}
		}
	}
	return err
}
