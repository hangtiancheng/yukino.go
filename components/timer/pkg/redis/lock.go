package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
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
	mu     sync.Mutex
	held   bool
}

func NewReentrantDistributeLock(key string, client *Client) *ReentrantDistributeLock {
	return &ReentrantDistributeLock{
		key:    key,
		client: client,
	}
}

func (r *ReentrantDistributeLock) Lock(ctx context.Context, expireSeconds int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client == nil || r.key == "" {
		return errors.New("lock requires a redis client and key")
	}
	if ctx == nil {
		return errors.New("nil lock context")
	}
	if r.held {
		return errors.New("lock is already held by this instance")
	}
	if expireSeconds <= 0 {
		return errors.New("lock expiration must be positive")
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("generate lock token: %w", err)
	}
	r.token = hex.EncodeToString(tokenBytes)
	reply, err := r.client.SetNX(ctx, r.getLockKey(), r.token, expireSeconds)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, _ = r.client.Eval(cleanupCtx, LuaCheckAndDeleteDistributionLock, 1, []any{r.getLockKey(), r.token})
		return err
	}

	re, _ := reply.(int64)
	if re != 1 {
		return errors.New("lock is acquired by others")
	}
	r.held = true

	return nil
}

func (r *ReentrantDistributeLock) Unlock(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client == nil || ctx == nil {
		return errors.New("redis client and context are required")
	}
	if !r.held {
		return errors.New("lock is not held by this instance")
	}
	keysAndArgs := []any{r.getLockKey(), r.token}
	reply, err := r.client.Eval(ctx, LuaCheckAndDeleteDistributionLock, 1, keysAndArgs)
	if err != nil {
		return err
	}

	if ret, _ := reply.(int64); ret != 1 {
		r.held = false
		return errors.New("can not unlock without ownership of lock")
	}
	r.held = false
	return nil
}

func (r *ReentrantDistributeLock) ExpireLock(ctx context.Context, expireSeconds int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client == nil || ctx == nil {
		return errors.New("redis client and context are required")
	}
	if !r.held {
		return errors.New("lock is not held by this instance")
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
		r.held = false
		return errors.New("can not expire lock without ownership of lock")
	}

	return nil
}

func (r *ReentrantDistributeLock) getLockKey() string {
	return ftimerLockKeyPrefix + r.key
}
