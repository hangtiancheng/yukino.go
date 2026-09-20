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
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/redis_lock"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/dao"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/mocks"
	"go.uber.org/mock/gomock"
)

func Test_MockTXStore_Lock(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockLock := mocks.NewMockLock(ctrl)
	mockLock.EXPECT().Lock(gomock.Any()).Return(nil)
	mockLock.EXPECT().Unlock(gomock.Any()).Return(nil)

	mockTXStore := &MockTXStore{
		dao:    mocks.NewMockTXRecordDAO(ctrl),
		client: mocks.NewMockRedisClient(ctrl),
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return mockLock
		},
	}

	ctx := context.Background()
	err := mockTXStore.Lock(ctx, time.Second)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	err = mockTXStore.Unlock(ctx)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func Test_MockTXStore_CreateTX(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDAO := mocks.NewMockTXRecordDAO(ctrl)
	mockLock := mocks.NewMockLock(ctrl)
	mockClient := mocks.NewMockRedisClient(ctrl)

	mockDAO.EXPECT().CreateTXRecord(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, record *dao.TXRecordPO) (uint, error) {
		if record.ComponentTryStatuses == "{}" {
			return 0, errors.New("invalid component try statuses")
		}
		return 1, nil
	}).AnyTimes()

	mockTXStore := &MockTXStore{
		dao:    mockDAO,
		client: mockClient,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return mockLock
		},
	}

	ctx := context.Background()
	_, err := mockTXStore.CreateTX(ctx)
	if err == nil {
		t.Error("expected error, got nil")
	}
	_, err = mockTXStore.CreateTX(ctx, NewMockComponent("id", nil))
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func Test_MockTXStore_TXUpdate(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDAO := mocks.NewMockTXRecordDAO(ctrl)
	mockLock := mocks.NewMockLock(ctrl)
	mockClient := mocks.NewMockRedisClient(ctrl)

	mockDAO.EXPECT().UpdateComponentStatus(gomock.Any(), uint(1), "component_id", tcc_demo.TXSuccessful.String()).Return(nil)

	mockTXStore := &MockTXStore{
		dao:    mockDAO,
		client: mockClient,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return mockLock
		},
	}

	err := mockTXStore.TXUpdate(context.Background(), "1", "component_id", true)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func Test_MockTXStore_GetHangingTXs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDAO := mocks.NewMockTXRecordDAO(ctrl)
	mockLock := mocks.NewMockLock(ctrl)
	mockClient := mocks.NewMockRedisClient(ctrl)

	componentTryStatuses := map[string]*dao.ComponentTryStatus{
		"component": {
			ComponentID: "component",
			TryStatus:   tcc_demo.TryHanging.String(),
		},
	}
	body, _ := json.Marshal(componentTryStatuses)

	mockDAO.EXPECT().GetTXRecords(gomock.Any(), gomock.Any()).Return([]*dao.TXRecordPO{
		{Status: tcc_demo.TXHanging.String(), ComponentTryStatuses: string(body)},
	}, nil)

	mockTXStore := &MockTXStore{
		dao:    mockDAO,
		client: mockClient,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return mockLock
		},
	}

	_, err := mockTXStore.GetHangingTXs(context.Background())
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func Test_MockTXStore_TXSubmit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDAO := mocks.NewMockTXRecordDAO(ctrl)
	mockUpdater := mocks.NewMockTXRecordUpdater(ctrl)
	mockLock := mocks.NewMockLock(ctrl)
	mockClient := mocks.NewMockRedisClient(ctrl)

	mockUpdater.EXPECT().UpdateTXRecord(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	mockDAO.EXPECT().LockAndDo(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id uint, do func(context.Context, dao.TXRecordUpdater, *dao.TXRecordPO) error) error {
			var record dao.TXRecordPO
			switch id {
			case 1:
				record = dao.TXRecordPO{Status: tcc_demo.TXSuccessful.String()}
			case 2:
				record = dao.TXRecordPO{Status: tcc_demo.TXFailure.String()}
			default:
				record = dao.TXRecordPO{Status: tcc_demo.TXHanging.String()}
			}
			return do(ctx, mockUpdater, &record)
		},
	).AnyTimes()

	mockTXStore := &MockTXStore{
		dao:    mockDAO,
		client: mockClient,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return mockLock
		},
	}

	ctx := context.Background()
	err := mockTXStore.TXSubmit(ctx, "1", false)
	if err == nil {
		t.Error("expected error, got nil")
	}
	err = mockTXStore.TXSubmit(ctx, "2", true)
	if err == nil {
		t.Error("expected error, got nil")
	}
	err = mockTXStore.TXSubmit(ctx, "3", true)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	err = mockTXStore.TXSubmit(ctx, "3", false)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func Test_MockTXStore_GetTX(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDAO := mocks.NewMockTXRecordDAO(ctrl)
	mockLock := mocks.NewMockLock(ctrl)
	mockClient := mocks.NewMockRedisClient(ctrl)

	componentTryStatuses := map[string]*dao.ComponentTryStatus{
		"component": {
			ComponentID: "component",
			TryStatus:   tcc_demo.TryHanging.String(),
		},
	}
	body, _ := json.Marshal(componentTryStatuses)

	mockDAO.EXPECT().GetTXRecords(gomock.Any(), gomock.Any()).Return([]*dao.TXRecordPO{
		{Status: tcc_demo.TXHanging.String(), ComponentTryStatuses: string(body)},
	}, nil)

	mockTXStore := &MockTXStore{
		dao:    mockDAO,
		client: mockClient,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return mockLock
		},
	}

	_, err := mockTXStore.GetTX(context.Background(), "1")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}
