package mocks

import (
	context "context"
	reflect "reflect"

	gomock "go.uber.org/mock/gomock"
)

type MockRedisClient struct {
	ctrl     *gomock.Controller
	recorder *MockRedisClientMockRecorder
	isgomock struct{}
}

type MockRedisClientMockRecorder struct {
	mock *MockRedisClient
}

func NewMockRedisClient(ctrl *gomock.Controller) *MockRedisClient {
	mock := &MockRedisClient{ctrl: ctrl}
	mock.recorder = &MockRedisClientMockRecorder{mock}
	return mock
}

func (m *MockRedisClient) EXPECT() *MockRedisClientMockRecorder {
	return m.recorder
}

func (m *MockRedisClient) Del(ctx context.Context, key string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Del", ctx, key)
	ret0, _ := ret[0].(error)
	return ret0
}

func (mr *MockRedisClientMockRecorder) Del(ctx, key any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Del", reflect.TypeOf((*MockRedisClient)(nil).Del), ctx, key)
}

func (m *MockRedisClient) Get(ctx context.Context, key string) (string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", ctx, key)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (mr *MockRedisClientMockRecorder) Get(ctx, key any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockRedisClient)(nil).Get), ctx, key)
}

func (m *MockRedisClient) Set(ctx context.Context, key, value string) (int64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Set", ctx, key, value)
	ret0, _ := ret[0].(int64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (mr *MockRedisClientMockRecorder) Set(ctx, key, value any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Set", reflect.TypeOf((*MockRedisClient)(nil).Set), ctx, key, value)
}

func (m *MockRedisClient) SetNX(ctx context.Context, key, value string) (int64, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetNX", ctx, key, value)
	ret0, _ := ret[0].(int64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (mr *MockRedisClientMockRecorder) SetNX(ctx, key, value any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetNX", reflect.TypeOf((*MockRedisClient)(nil).SetNX), ctx, key, value)
}

type MockLock struct {
	ctrl     *gomock.Controller
	recorder *MockLockMockRecorder
	isgomock struct{}
}

type MockLockMockRecorder struct {
	mock *MockLock
}

func NewMockLock(ctrl *gomock.Controller) *MockLock {
	mock := &MockLock{ctrl: ctrl}
	mock.recorder = &MockLockMockRecorder{mock}
	return mock
}

func (m *MockLock) EXPECT() *MockLockMockRecorder {
	return m.recorder
}

func (m *MockLock) Lock(ctx context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Lock", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

func (mr *MockLockMockRecorder) Lock(ctx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Lock", reflect.TypeOf((*MockLock)(nil).Lock), ctx)
}

func (m *MockLock) Unlock(ctx context.Context) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Unlock", ctx)
	ret0, _ := ret[0].(error)
	return ret0
}

func (mr *MockLockMockRecorder) Unlock(ctx any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Unlock", reflect.TypeOf((*MockLock)(nil).Unlock), ctx)
}
