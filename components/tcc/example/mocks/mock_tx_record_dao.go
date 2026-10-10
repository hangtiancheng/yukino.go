package mocks

import (
	context "context"
	reflect "reflect"

	dao "github.com/hangtiancheng/yukino.go/components/tcc/example/dao"
	gomock "go.uber.org/mock/gomock"
)

type MockTXRecordDAO struct {
	ctrl     *gomock.Controller
	recorder *MockTXRecordDAOMockRecorder
	isgomock struct{}
}

type MockTXRecordDAOMockRecorder struct {
	mock *MockTXRecordDAO
}

func NewMockTXRecordDAO(ctrl *gomock.Controller) *MockTXRecordDAO {
	mock := &MockTXRecordDAO{ctrl: ctrl}
	mock.recorder = &MockTXRecordDAOMockRecorder{mock}
	return mock
}

func (m *MockTXRecordDAO) EXPECT() *MockTXRecordDAOMockRecorder {
	return m.recorder
}

func (m *MockTXRecordDAO) CreateTXRecord(ctx context.Context, record *dao.TXRecordPO) (uint, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateTXRecord", ctx, record)
	ret0, _ := ret[0].(uint)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (mr *MockTXRecordDAOMockRecorder) CreateTXRecord(ctx, record any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateTXRecord", reflect.TypeOf((*MockTXRecordDAO)(nil).CreateTXRecord), ctx, record)
}

func (m *MockTXRecordDAO) GetTXRecords(ctx context.Context, opts ...dao.QueryOption) ([]*dao.TXRecordPO, error) {
	m.ctrl.T.Helper()
	varargs := []any{ctx}
	for _, a := range opts {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "GetTXRecords", varargs...)
	ret0, _ := ret[0].([]*dao.TXRecordPO)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (mr *MockTXRecordDAOMockRecorder) GetTXRecords(ctx any, opts ...any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{ctx}, opts...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetTXRecords", reflect.TypeOf((*MockTXRecordDAO)(nil).GetTXRecords), varargs...)
}

func (m *MockTXRecordDAO) LockAndDo(ctx context.Context, id uint, do func(context.Context, dao.TXRecordUpdater, *dao.TXRecordPO) error) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LockAndDo", ctx, id, do)
	ret0, _ := ret[0].(error)
	return ret0
}

func (mr *MockTXRecordDAOMockRecorder) LockAndDo(ctx, id, do any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LockAndDo", reflect.TypeOf((*MockTXRecordDAO)(nil).LockAndDo), ctx, id, do)
}

func (m *MockTXRecordDAO) UpdateComponentStatus(ctx context.Context, id uint, componentID, status string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateComponentStatus", ctx, id, componentID, status)
	ret0, _ := ret[0].(error)
	return ret0
}

func (mr *MockTXRecordDAOMockRecorder) UpdateComponentStatus(ctx, id, componentID, status any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateComponentStatus", reflect.TypeOf((*MockTXRecordDAO)(nil).UpdateComponentStatus), ctx, id, componentID, status)
}

func (m *MockTXRecordDAO) UpdateTXRecord(ctx context.Context, record *dao.TXRecordPO) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateTXRecord", ctx, record)
	ret0, _ := ret[0].(error)
	return ret0
}

func (mr *MockTXRecordDAOMockRecorder) UpdateTXRecord(ctx, record any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateTXRecord", reflect.TypeOf((*MockTXRecordDAO)(nil).UpdateTXRecord), ctx, record)
}
