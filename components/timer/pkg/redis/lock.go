package redis

import (
	"context"
	"errors"

	"github.com/hangtiancheng/yukino.go/components/timer/common/utils"
	go_redis "github.com/redis/go-redis/v9"
)

const ftimerLockKeyPrefix = "FTIMER_LOCK_PREFIX_"

type DistributeLocker interface {
	Lock(context.Context, int64) error
	Unlock(context.Context) error
	ExpireLock(ctx context.Context, expireSeconds int64) error
}

type ReentrantDistributeLock struct {
	key    string
	token  string
	client *Client
}

func NewReentrantDistributeLock(key string, client *Client) *ReentrantDistributeLock {
	return &ReentrantDistributeLock{
		key:    key,
		token:  utils.GetProcessAndGoroutineIDStr(),
		client: client,
	}
}

func (r *ReentrantDistributeLock) Lock(ctx context.Context, expireSeconds int64) error {
	res, err := r.client.Get(ctx, r.key)
	if err != nil && !errors.Is(err, go_redis.Nil) {
		return err
	}

	if res == r.token {
		if err := r.ExpireLock(ctx, expireSeconds); err != nil {
			return err
		}
		return nil
	}

	reply, err := r.client.SetNX(ctx, r.getLockKey(), r.token, expireSeconds)
	if err != nil {
		return err
	}

	re, _ := reply.(int64)
	if re != 1 {
		return errors.New("lock is acquired by others")
	}

	return nil
}

func (r *ReentrantDistributeLock) Unlock(ctx context.Context) error {
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

func (r *ReentrantDistributeLock) ExpireLock(ctx context.Context, expireSeconds int64) error {
	keysAndArgs := []any{r.getLockKey(), r.token, expireSeconds}
	reply, err := r.client.Eval(ctx, LuaCheckAndExpireDistributionLock, 1, keysAndArgs)
	if err != nil {
		return err
	}

	if ret, _ := reply.(int64); ret != 1 {
		return errors.New("can not expire lock without ownership of lock")
	}

	return nil
}

func (r *ReentrantDistributeLock) getLockKey() string {
	return ftimerLockKeyPrefix + r.key
}
