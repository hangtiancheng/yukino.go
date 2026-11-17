package example

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/components/redis_lock"
	"github.com/hangtiancheng/yukino.go/components/tcc"
	"github.com/hangtiancheng/yukino.go/components/tcc/example/dao"
	"github.com/hangtiancheng/yukino.go/components/tcc/example/mocks"
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

	mockDAO.EXPECT().UpdateComponentStatus(gomock.Any(), uint(1), "component_id", tcc.TXSuccessful.String()).Return(nil)

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
			TryStatus:   tcc.TryHanging.String(),
		},
	}
	body, _ := json.Marshal(componentTryStatuses)

	mockDAO.EXPECT().GetTXRecords(gomock.Any(), gomock.Any()).Return([]*dao.TXRecordPO{
		{Status: tcc.TXHanging.String(), ComponentTryStatuses: string(body)},
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
				record = dao.TXRecordPO{Status: tcc.TXSuccessful.String()}
			case 2:
				record = dao.TXRecordPO{Status: tcc.TXFailure.String()}
			default:
				record = dao.TXRecordPO{Status: tcc.TXHanging.String()}
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
			TryStatus:   tcc.TryHanging.String(),
		},
	}
	body, _ := json.Marshal(componentTryStatuses)

	mockDAO.EXPECT().GetTXRecords(gomock.Any(), gomock.Any()).Return([]*dao.TXRecordPO{
		{Status: tcc.TXHanging.String(), ComponentTryStatuses: string(body)},
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
