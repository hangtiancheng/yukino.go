package mocks

import (
	context "context"
	reflect "reflect"

	dao "github.com/hangtiancheng/yukino.go/components/tcc/example/dao"
	gomock "go.uber.org/mock/gomock"
)

type MockTXRecordUpdater struct {
	ctrl     *gomock.Controller
	recorder *MockTXRecordUpdaterMockRecorder
	isgomock struct{}
}

type MockTXRecordUpdaterMockRecorder struct {
	mock *MockTXRecordUpdater
}

func NewMockTXRecordUpdater(ctrl *gomock.Controller) *MockTXRecordUpdater {
	mock := &MockTXRecordUpdater{ctrl: ctrl}
	mock.recorder = &MockTXRecordUpdaterMockRecorder{mock}
	return mock
}

func (m *MockTXRecordUpdater) EXPECT() *MockTXRecordUpdaterMockRecorder {
	return m.recorder
}

func (m *MockTXRecordUpdater) UpdateTXRecord(ctx context.Context, record *dao.TXRecordPO) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateTXRecord", ctx, record)
	ret0, _ := ret[0].(error)
	return ret0
}

func (mr *MockTXRecordUpdaterMockRecorder) UpdateTXRecord(ctx, record any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateTXRecord", reflect.TypeOf((*MockTXRecordUpdater)(nil).UpdateTXRecord), ctx, record)
}
