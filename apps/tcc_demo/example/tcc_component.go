// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package example

import (
	"context"
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/redis_lock"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/pkg"
	go_redis "github.com/redis/go-redis/v9"
)

// Transaction status recorded on the TCC component side
type TXStatus string

func (t TXStatus) String() string {
	return string(t)
}

const (
	TXTried     TXStatus = "tried"     // Try phase completed
	TXConfirmed TXStatus = "confirmed" // Confirm phase completed
	TXCanceled  TXStatus = "canceled"  // Cancel phase completed
)

// Data status for a transaction
type DataStatus string

func (d DataStatus) String() string {
	return string(d)
}

const (
	DataFrozen     DataStatus = "frozen"     // Frozen state
	DataSuccessful DataStatus = "successful" // Successful state
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

func (m *MockComponent) Try(ctx context.Context, req *tcc_demo.TCCReq) (*tcc_demo.TCCResp, error) {
	// Acquire lock by txID
	lock := m.lockFactory(pkg.BuildTXLockKey(m.id, req.TXID))
	if err := lock.Lock(ctx); err != nil {
		return nil, err
	}
	defer func() {
		_ = lock.Unlock(ctx)
	}()

	// Idempotency check by txID
	txStatus, err := m.client.Get(ctx, pkg.BuildTXKey(m.id, req.TXID))
	if err != nil && !errors.Is(err, go_redis.Nil) {
		return nil, err
	}

	res := tcc_demo.TCCResp{
		ComponentID: m.id,
		TXID:        req.TXID,
	}
	switch txStatus {
	case TXTried.String(), TXConfirmed.String(): // Duplicate try request, return success
		res.ACK = true
		return &res, nil
	case TXCanceled.String(): // Cancel received before try, reject
		return &res, nil
	default:
	}

	// Execute try operation, set data to frozen state
	bizID := fmt.Sprintf("%v", req.Data["biz_id"])
	// Store bizID-transaction mapping
	if _, err = m.client.Set(ctx, pkg.BuildTXDetailKey(m.id, req.TXID), bizID); err != nil {
		return nil, err
	}

	// Atomically set bizID data to frozen state
	reply, err := m.client.SetNX(ctx, pkg.BuildDataKey(m.id, req.TXID, bizID), DataFrozen.String())
	if err != nil {
		return nil, err
	}
	if reply != 1 {
		return &res, nil
	}

	// Update transaction status
	if _, err = m.client.Set(ctx, pkg.BuildTXKey(m.id, req.TXID), TXTried.String()); err != nil {
		return nil, err
	}

	// Try request succeeded
	res.ACK = true
	return &res, nil
}

func (m *MockComponent) Confirm(ctx context.Context, txID string) (*tcc_demo.TCCResp, error) {
	// Acquire lock by txID
	lock := m.lockFactory(pkg.BuildTXLockKey(m.id, txID))
	if err := lock.Lock(ctx); err != nil {
		return nil, err
	}
	defer func() {
		_ = lock.Unlock(ctx)
	}()

	// 1. Require txID status to be "tried"
	txStatus, err := m.client.Get(ctx, pkg.BuildTXKey(m.id, txID))
	if err != nil {
		return nil, err
	}

	res := tcc_demo.TCCResp{
		ComponentID: m.id,
		TXID:        txID,
	}
	switch txStatus {
	case TXConfirmed.String(): // Already confirmed, return idempotent success
		res.ACK = true
		return &res, nil
	case TXTried.String(): // Only allow if status is "tried"
	default: // Reject for any other status
		return &res, nil
	}

	// Get bizID for the transaction
	bizID, err := m.client.Get(ctx, pkg.BuildTXDetailKey(m.id, txID))
	if err != nil {
		return nil, err
	}

	// 2. Require data status to be "frozen"
	dataStatus, err := m.client.Get(ctx, pkg.BuildDataKey(m.id, txID, bizID))
	if err != nil {
		return nil, err
	}
	if dataStatus != DataFrozen.String() {
		// Invalid data status, reject
		return &res, nil
	}

	// Set data status to "successful"
	if _, err = m.client.Set(ctx, pkg.BuildDataKey(m.id, txID, bizID), DataSuccessful.String()); err != nil {
		return nil, err
	}

	// Update transaction status to confirmed; failure here does not block the main flow
	_, _ = m.client.Set(ctx, pkg.BuildTXKey(m.id, txID), TXConfirmed.String())

	// Confirm succeeded, return success
	res.ACK = true
	return &res, nil
}

func (m *MockComponent) Cancel(ctx context.Context, txID string) (*tcc_demo.TCCResp, error) {
	// Acquire lock by txID
	lock := m.lockFactory(pkg.BuildTXLockKey(m.id, txID))
	if err := lock.Lock(ctx); err != nil {
		return nil, err
	}
	defer func() {
		_ = lock.Unlock(ctx)
	}()

	// Check transaction status; if not confirmed, unconditionally set to cancelled
	txStatus, err := m.client.Get(ctx, pkg.BuildTXKey(m.id, txID))
	if err != nil && !errors.Is(err, go_redis.Nil) {
		return nil, err
	}
	// Confirm followed by cancel is an illegal state transition
	if txStatus == TXConfirmed.String() {
		return nil, fmt.Errorf("invalid tx status: %s, txid: %s", txStatus, txID)
	}

	// Get bizID for the transaction
	bizID, err := m.client.Get(ctx, pkg.BuildTXDetailKey(m.id, txID))
	if err != nil {
		return nil, err
	}

	// Delete the frozen data record
	if err = m.client.Del(ctx, pkg.BuildDataKey(m.id, txID, bizID)); err != nil {
		return nil, err
	}

	// Update transaction status to cancelled
	_, _ = m.client.Set(ctx, pkg.BuildTXKey(m.id, txID), TXCanceled.String())

	return &tcc_demo.TCCResp{
		ACK:         true,
		ComponentID: m.id,
		TXID:        txID,
	}, nil
}
