package redis_lock

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"
)

const DefaultSingleLockTimeout = 50 * time.Millisecond

// RedLock uses independent masters and a fixed lease. All nodes share one
// random token per attempt. The validity window subtracts total elapsed time
// and clock drift, including unsuccessful nodes. It must not be copied.
type RedLock struct {
	mu           sync.Mutex
	clients      []LockClient
	ownedClients bool
	key          string
	token        string
	validUntil   time.Time
	held         bool
	closed       bool
	RedLockOptions
}

func NewRedLock(key string, confs []*SingleNodeConf, opts ...RedLockOption) (*RedLock, error) {
	addresses := make(map[string]bool, len(confs))
	for _, conf := range confs {
		if conf == nil || conf.Address == "" {
			return nil, errors.New("redlock node address is empty")
		}
		network := conf.Network
		if network == "" {
			network = "tcp"
		}
		address := network + "://" + conf.Address
		if addresses[address] {
			return nil, fmt.Errorf("duplicate redlock node: %s", address)
		}
		addresses[address] = true
	}
	clients := make([]LockClient, len(confs))
	for i, conf := range confs {
		clients[i] = NewClient(conf.Network, conf.Address, conf.Password, conf.Opts...)
	}
	r, err := NewRedLockWithClients(key, clients, opts...)
	if err != nil {
		for _, client := range clients {
			_ = client.(*Client).Close()
		}
		return nil, err
	}
	r.ownedClients = true
	return r, nil
}

// NewRedLockWithClients borrows clients; Close does not close them. Clients
// must point at different independent Redis masters and obey context deadlines.
func NewRedLockWithClients(key string, clients []LockClient, opts ...RedLockOption) (*RedLock, error) {
	if key == "" || len(clients) < 3 {
		return nil, errors.New("redlock requires key and at least 3 independent clients")
	}
	r := &RedLock{clients: append([]LockClient(nil), clients...)}
	seen := make(map[LockClient]bool)
	for _, client := range clients {
		if client == nil || (reflect.ValueOf(client).Kind() == reflect.Pointer && reflect.ValueOf(client).IsNil()) {
			return nil, errors.New("nil redlock client")
		}
		if reflect.TypeOf(client).Comparable() {
			if seen[client] {
				return nil, errors.New("duplicate redlock client")
			}
			seen[client] = true
		}
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&r.RedLockOptions)
		}
	}
	repairRedLock(&r.RedLockOptions)
	r.key = RedisLockKeyPrefix + key
	if r.expireDuration < time.Second || r.expireDuration%time.Second != 0 {
		return nil, errors.New("redlock expiration must be positive whole seconds")
	}
	if r.singleNodesTimeout >= r.expireDuration/10 {
		return nil, errors.New("redlock node timeout must be less than TTL / 10")
	}
	return r, nil
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
	if r.token != "" && time.Now().Before(r.validUntil) {
		return ErrAlreadyHeld
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	r.token = token
	r.held = false
	started := time.Now()
	replies := make([]int64, len(r.clients))
	errs := make([]error, len(r.clients))
	var wg sync.WaitGroup
	for i, client := range r.clients {
		wg.Go(func() {
			nodeCtx, cancel := context.WithTimeout(ctx, r.singleNodesTimeout)
			defer cancel()
			replies[i], errs[i] = client.SetNEX(nodeCtx, r.key, token, int64(r.expireDuration/time.Second))
			if errs[i] == nil && nodeCtx.Err() != nil {
				errs[i] = nodeCtx.Err()
			}
		})
	}
	wg.Wait()
	r.validUntil = started.Add(r.expireDuration - leaseDrift(r.expireDuration))
	successes := 0
	for i, reply := range replies {
		if reply == 1 && errs[i] == nil {
			successes++
		}
	}
	if successes >= len(r.clients)/2+1 && ctx.Err() == nil && time.Now().Before(r.validUntil) {
		r.held = true
		return nil
	}
	failure := errors.Join(append([]error{ErrLockAcquiredByOthers, ctx.Err()}, errs...)...)
	return errors.Join(failure, r.release(ctx))
}

// release always contacts ALL masters, including nodes whose SET returned an
// error: the server may have written the key before the reply was lost.
func (r *RedLock) release(ctx context.Context) error {
	if r.token == "" {
		return nil
	}
	errs := make([]error, len(r.clients))
	var wg sync.WaitGroup
	for i, client := range r.clients {
		wg.Go(func() {
			nodeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.singleNodesTimeout)
			defer cancel()
			reply, err := client.Eval(nodeCtx, LuaCheckAndDeleteDistributionLock, 1, []any{r.key, r.token})
			if err == nil {
				ret, ok := reply.(int64)
				if !ok || (ret != 0 && ret != 1) {
					err = fmt.Errorf("unexpected redis release reply: %v", reply)
				}
			}
			errs[i] = err
		})
	}
	wg.Wait()
	r.held = false
	err := errors.Join(errs...)
	if err == nil {
		r.token = ""
		r.validUntil = time.Time{}
	}
	return err
}
func (r *RedLock) Unlock(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.release(ctx)
}

// DelayExpire requires a fresh quorum before the current lease expires.
func (r *RedLock) DelayExpire(ctx context.Context, seconds int64) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if seconds <= 0 || seconds > int64((1<<63-1)/time.Second) {
		return errors.New("expiration must fit a positive time.Duration")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held || !time.Now().Before(r.validUntil) {
		r.held = false
		return ErrLockLost
	}
	previous, started := r.validUntil, time.Now()
	errs := make([]error, len(r.clients))
	replies := make([]bool, len(r.clients))
	var wg sync.WaitGroup
	for i, client := range r.clients {
		wg.Go(func() {
			nodeCtx, cancel := context.WithTimeout(ctx, r.singleNodesTimeout)
			defer cancel()
			reply, err := client.Eval(nodeCtx, LuaCheckAndExpireDistributionLock, 1, []any{r.key, r.token, seconds})
			errs[i] = err
			ret, ok := reply.(int64)
			replies[i] = err == nil && nodeCtx.Err() == nil && ok && ret == 1
		})
	}
	wg.Wait()
	ttl := time.Duration(seconds) * time.Second
	r.validUntil = started.Add(ttl - leaseDrift(ttl))
	successes := 0
	for _, ok := range replies {
		if ok {
			successes++
		}
	}
	if successes < len(r.clients)/2+1 || ctx.Err() != nil || !time.Now().Before(previous) || !time.Now().Before(r.validUntil) {
		r.held = false
		return errors.Join(append([]error{ErrLockLost, ctx.Err()}, errs...)...)
	}
	return nil
}
func (r *RedLock) Held() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.held && time.Now().Before(r.validUntil)
}
func (r *RedLock) Remaining() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held {
		return 0
	}
	return max(0, time.Until(r.validUntil))
}
func (r *RedLock) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	err := r.release(context.Background())
	if r.ownedClients {
		for _, client := range r.clients {
			err = errors.Join(err, client.(*Client).Close())
		}
		r.ownedClients = false
	}
	return err
}
