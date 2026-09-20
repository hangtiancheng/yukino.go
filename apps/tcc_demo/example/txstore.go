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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/redis_lock"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo"
	example_dao "github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/dao"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/pkg"
)

type MockTXStore struct {
	client      RedisClient
	dao         TXRecordDAO
	lockFactory LockFactory
}

func NewMockTXStore(dao TXRecordDAO, client *redis_lock.Client) *MockTXStore {
	return &MockTXStore{
		dao:    dao,
		client: client,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return redis_lock.NewRedisLock(key, client, opts...)
		},
	}
}

func (m *MockTXStore) CreateTX(ctx context.Context, components ...tcc_demo.TCCComponent) (string, error) {
	// Create a record keyed by the unique transaction ID
	componentTryStatuses := make(map[string]*example_dao.ComponentTryStatus, len(components))
	for _, component := range components {
		componentTryStatuses[component.ID()] = &example_dao.ComponentTryStatus{
			ComponentID: component.ID(),
			TryStatus:   tcc_demo.TryHanging.String(),
		}
	}

	statusesBody, _ := json.Marshal(componentTryStatuses)
	txID, err := m.dao.CreateTXRecord(ctx, &example_dao.TXRecordPO{
		Status:               tcc_demo.TXHanging.String(),
		ComponentTryStatuses: string(statusesBody),
	})
	if err != nil {
		return "", err
	}

	return strconv.FormatUint(uint64(txID), 10), nil
}

func (m *MockTXStore) TXUpdate(ctx context.Context, txID string, componentID string, accept bool) error {
	parsedID, err := strconv.ParseUint(txID, 10, 64)
	if err != nil {
		return err
	}
	_txID := uint(parsedID)
	status := tcc_demo.TXFailure.String()
	if accept {
		status = tcc_demo.TXSuccessful.String()
	}
	return m.dao.UpdateComponentStatus(ctx, _txID, componentID, status)
}

func (m *MockTXStore) GetHangingTXs(ctx context.Context) ([]*tcc_demo.Transaction, error) {
	records, err := m.dao.GetTXRecords(ctx, example_dao.WithStatus(tcc_demo.TryHanging))
	if err != nil {
		return nil, err
	}

	txs := make([]*tcc_demo.Transaction, 0, len(records))
	for _, record := range records {
		componentTryStatuses := make(map[string]*example_dao.ComponentTryStatus)
		_ = json.Unmarshal([]byte(record.ComponentTryStatuses), &componentTryStatuses)
		components := make([]*tcc_demo.ComponentTryEntity, 0, len(componentTryStatuses))
		for _, component := range componentTryStatuses {
			components = append(components, &tcc_demo.ComponentTryEntity{
				ComponentID: component.ComponentID,
				TryStatus:   tcc_demo.ComponentTryStatus(component.TryStatus),
			})
		}

		txs = append(txs, &tcc_demo.Transaction{
			TXID:       strconv.FormatUint(uint64(record.ID), 10),
			Status:     tcc_demo.TXHanging,
			CreatedAt:  record.CreatedAt,
			Components: components,
		})
	}

	return txs, nil
}

func (m *MockTXStore) Lock(ctx context.Context, expireDuration time.Duration) error {
	lock := m.lockFactory(pkg.BuildTXRecordLockKey(), redis_lock.WithExpireSeconds(int64(expireDuration.Seconds())))
	return lock.Lock(ctx)
}

func (m *MockTXStore) Unlock(ctx context.Context) error {
	lock := m.lockFactory(pkg.BuildTXRecordLockKey())
	return lock.Unlock(ctx)
}

// Submits the final transaction status
func (m *MockTXStore) TXSubmit(ctx context.Context, txID string, success bool) error {
	do := func(ctx context.Context, dao example_dao.TXRecordUpdater, record *example_dao.TXRecordPO) error {
		if success {
			if record.Status == tcc_demo.TXFailure.String() {
				return fmt.Errorf("invalid tx status: %s, txid: %s", record.Status, txID)
			}
			record.Status = tcc_demo.TXSuccessful.String()
		} else {
			if record.Status == tcc_demo.TXSuccessful.String() {
				return fmt.Errorf("invalid tx status: %s, txid: %s", record.Status, txID)
			}
			record.Status = tcc_demo.TXFailure.String()
		}
		return dao.UpdateTXRecord(ctx, record)
	}
	parsedID, err := strconv.ParseUint(txID, 10, 64)
	if err != nil {
		return err
	}
	return m.dao.LockAndDo(ctx, uint(parsedID), do)
}

// Retrieves a specific transaction by ID
func (m *MockTXStore) GetTX(ctx context.Context, txID string) (*tcc_demo.Transaction, error) {
	parsedID, err := strconv.ParseUint(txID, 10, 64)
	if err != nil {
		return nil, err
	}
	records, err := m.dao.GetTXRecords(ctx, example_dao.WithID(uint(parsedID)))
	if err != nil {
		return nil, err
	}
	if len(records) != 1 {
		return nil, errors.New("get tx failed")
	}

	componentTryStatuses := make(map[string]*example_dao.ComponentTryStatus)
	_ = json.Unmarshal([]byte(records[0].ComponentTryStatuses), &componentTryStatuses)

	components := make([]*tcc_demo.ComponentTryEntity, 0, len(componentTryStatuses))
	for _, tryItem := range componentTryStatuses {
		components = append(components, &tcc_demo.ComponentTryEntity{
			ComponentID: tryItem.ComponentID,
			TryStatus:   tcc_demo.ComponentTryStatus(tryItem.TryStatus),
		})
	}
	return &tcc_demo.Transaction{
		TXID:       txID,
		Status:     tcc_demo.TXStatus(records[0].Status),
		Components: components,
		CreatedAt:  records[0].CreatedAt,
	}, nil
}

type TXRecordDAO interface {
	GetTXRecords(ctx context.Context, opts ...example_dao.QueryOption) ([]*example_dao.TXRecordPO, error)
	CreateTXRecord(ctx context.Context, record *example_dao.TXRecordPO) (uint, error)
	UpdateComponentStatus(ctx context.Context, id uint, componentID string, status string) error
	UpdateTXRecord(ctx context.Context, record *example_dao.TXRecordPO) error
	LockAndDo(ctx context.Context, id uint, do func(ctx context.Context, dao example_dao.TXRecordUpdater, record *example_dao.TXRecordPO) error) error
}
