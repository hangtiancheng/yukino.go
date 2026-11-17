package example

import (
	"context"
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino.go/components/redis_lock"
	"github.com/hangtiancheng/yukino.go/components/tcc"
	"github.com/hangtiancheng/yukino.go/components/tcc/example/pkg"
	go_redis "github.com/redis/go-redis/v9"
)

type TXStatus string

func (t TXStatus) String() string {
	return string(t)
}

const (
	TXTried     TXStatus = "tried"
	TXConfirmed TXStatus = "confirmed"
	TXCanceled  TXStatus = "canceled"
)

type DataStatus string

func (d DataStatus) String() string {
	return string(d)
}

const (
	DataFrozen     DataStatus = "frozen"
	DataSuccessful DataStatus = "successful"
)

type MockComponent struct {
	id          string
	client      RedisClient
	lockFactory LockFactory
}

func NewMockComponent(id string, client *redis_lock.Client) *MockComponent {
	return &MockComponent{
		id:     id,
		client: client,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return redis_lock.NewRedisLock(key, client, opts...)
		},
	}
}

func (m *MockComponent) ID() string {
	return m.id
}

func (m *MockComponent) Try(ctx context.Context, req *tcc.TCCReq) (*tcc.TCCResp, error) {
	lock := m.lockFactory(pkg.BuildTXLockKey(m.id, req.TXID))
	if err := lock.Lock(ctx); err != nil {
		return nil, err
	}
	defer func() {
		_ = lock.Unlock(ctx)
	}()

	txStatus, err := m.client.Get(ctx, pkg.BuildTXKey(m.id, req.TXID))
	if err != nil && !errors.Is(err, go_redis.Nil) {
		return nil, err
	}

	res := tcc.TCCResp{
		ComponentID: m.id,
		TXID:        req.TXID,
	}
	switch txStatus {
	case TXTried.String(), TXConfirmed.String():
		res.ACK = true
		return &res, nil
	case TXCanceled.String():
		return &res, nil
	default:
	}

	bizID := fmt.Sprintf("%v", req.Data["biz_id"])
	if _, err = m.client.Set(ctx, pkg.BuildTXDetailKey(m.id, req.TXID), bizID); err != nil {
		return nil, err
	}

	reply, err := m.client.SetNX(ctx, pkg.BuildDataKey(m.id, req.TXID, bizID), DataFrozen.String())
	if err != nil {
		return nil, err
	}
	if reply != 1 {
		return &res, nil
	}

	if _, err = m.client.Set(ctx, pkg.BuildTXKey(m.id, req.TXID), TXTried.String()); err != nil {
		return nil, err
	}

	res.ACK = true
	return &res, nil
}

func (m *MockComponent) Confirm(ctx context.Context, txID string) (*tcc.TCCResp, error) {
	lock := m.lockFactory(pkg.BuildTXLockKey(m.id, txID))
	if err := lock.Lock(ctx); err != nil {
		return nil, err
	}
	defer func() {
		_ = lock.Unlock(ctx)
	}()

	txStatus, err := m.client.Get(ctx, pkg.BuildTXKey(m.id, txID))
	if err != nil {
		return nil, err
	}

	res := tcc.TCCResp{
		ComponentID: m.id,
		TXID:        txID,
	}
	switch txStatus {
	case TXConfirmed.String():
		res.ACK = true
		return &res, nil
	case TXTried.String():
	default:
		return &res, nil
	}

	bizID, err := m.client.Get(ctx, pkg.BuildTXDetailKey(m.id, txID))
	if err != nil {
		return nil, err
	}

	dataStatus, err := m.client.Get(ctx, pkg.BuildDataKey(m.id, txID, bizID))
	if err != nil {
		return nil, err
	}
	if dataStatus != DataFrozen.String() {
		return &res, nil
	}

	if _, err = m.client.Set(ctx, pkg.BuildDataKey(m.id, txID, bizID), DataSuccessful.String()); err != nil {
		return nil, err
	}

	_, _ = m.client.Set(ctx, pkg.BuildTXKey(m.id, txID), TXConfirmed.String())

	res.ACK = true
	return &res, nil
}

func (m *MockComponent) Cancel(ctx context.Context, txID string) (*tcc.TCCResp, error) {
	lock := m.lockFactory(pkg.BuildTXLockKey(m.id, txID))
	if err := lock.Lock(ctx); err != nil {
		return nil, err
	}
	defer func() {
		_ = lock.Unlock(ctx)
	}()

	txStatus, err := m.client.Get(ctx, pkg.BuildTXKey(m.id, txID))
	if err != nil && !errors.Is(err, go_redis.Nil) {
		return nil, err
	}
	if txStatus == TXConfirmed.String() {
		return nil, fmt.Errorf("invalid tx status: %s, txid: %s", txStatus, txID)
	}

	bizID, err := m.client.Get(ctx, pkg.BuildTXDetailKey(m.id, txID))
	if err != nil {
		return nil, err
	}

	if err = m.client.Del(ctx, pkg.BuildDataKey(m.id, txID, bizID)); err != nil {
		return nil, err
	}

	_, _ = m.client.Set(ctx, pkg.BuildTXKey(m.id, txID), TXCanceled.String())

	return &tcc.TCCResp{
		ACK:         true,
		ComponentID: m.id,
		TXID:        txID,
	}, nil
}
