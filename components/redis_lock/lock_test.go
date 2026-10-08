package redis_lock

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func Test_blockingLock(t *testing.T) {
	// Fill in the redis node address and password.
	addr := os.Getenv("REDIS_LOCK_ADDR")
	passwd := os.Getenv("REDIS_LOCK_PASSWORD")
	if addr == "" {
		t.Skip("set REDIS_LOCK_ADDR (and optional REDIS_LOCK_PASSWORD) to run this test against a real redis")
	}

	client := NewClient("tcp", addr, passwd)
	lock1 := NewRedisLock("test_key", client, WithExpireSeconds(1))
	lock2 := NewRedisLock("test_key", client, WithBlock(), WithBlockWaitingSeconds(2))

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Go(func() {
		if err := lock1.Lock(ctx); err != nil {
			t.Error(err)
			return
		}
	})

	wg.Go(func() {
		if err := lock2.Lock(ctx); err != nil {
			t.Error(err)
			return
		}
	})

	wg.Wait()

	t.Log("success")
}

func Test_nonBlockingLock(t *testing.T) {
	// Fill in the redis node address and password.
	addr := os.Getenv("REDIS_LOCK_ADDR")
	passwd := os.Getenv("REDIS_LOCK_PASSWORD")
	if addr == "" {
		t.Skip("set REDIS_LOCK_ADDR (and optional REDIS_LOCK_PASSWORD) to run this test against a real redis")
	}

	client := NewClient("tcp", addr, passwd)
	lock1 := NewRedisLock("test_key", client, WithExpireSeconds(1))
	lock2 := NewRedisLock("test_key", client)

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Go(func() {
		if err := lock1.Lock(ctx); err != nil {
			t.Error(err)
			return
		}
	})

	wg.Go(func() {
		if err := lock2.Lock(ctx); err == nil || !errors.Is(err, ErrLockAcquiredByOthers) {
			t.Errorf("got err: %v, expect: %v", err, ErrLockAcquiredByOthers)
			return
		}
	})

	wg.Wait()
	t.Log("success")
}

func Test_redLock(t *testing.T) {
	// Fill in the addresses and passwords of three redis nodes.
	addrs := strings.Split(os.Getenv("REDIS_LOCK_ADDRS"), ",")
	passwd := os.Getenv("REDIS_LOCK_PASSWORD")
	if len(addrs) < 3 || addrs[0] == "" {
		t.Skip("set REDIS_LOCK_ADDRS (comma separated, at least 3 nodes) and optional REDIS_LOCK_PASSWORD to run this test against real redis nodes")
	}

	// Three locks, one per direct redis node.
	confs := make([]*SingleNodeConf, 0, len(addrs))
	for _, addr := range addrs {
		confs = append(confs, &SingleNodeConf{
			Network:  "tcp",
			Address:  addr,
			Password: passwd,
		})
	}

	redLock, err := NewRedLock("test_key", confs, WithRedLockExpireDuration(10*time.Second), WithSingleNodesTimeout(100*time.Millisecond))
	if err != nil {
		return
	}

	ctx := context.Background()
	if err = redLock.Lock(ctx); err != nil {
		t.Error(err)
		return
	}

	if err = redLock.Unlock(ctx); err != nil {
		t.Error(err)
		return
	}

	t.Log("success")
}
