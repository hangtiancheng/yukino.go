package mocks

import (
	reflect "reflect"

	gomock "go.uber.org/mock/gomock"
	gorm "gorm.io/gorm"
)

type MockDBFactory struct {
	ctrl     *gomock.Controller
	recorder *MockDBFactoryMockRecorder
	isgomock struct{}
}

type MockDBFactoryMockRecorder struct {
	mock *MockDBFactory
}

func NewMockDBFactory(ctrl *gomock.Controller) *MockDBFactory {
	mock := &MockDBFactory{ctrl: ctrl}
	mock.recorder = &MockDBFactoryMockRecorder{mock}
	return mock
}

func (m *MockDBFactory) EXPECT() *MockDBFactoryMockRecorder {
	return m.recorder
}

func (m *MockDBFactory) Open(dsn string, opts ...gorm.Option) (*gorm.DB, error) {
	m.ctrl.T.Helper()
	varargs := []any{dsn}
	for _, a := range opts {
		varargs = append(varargs, a)
	}
	ret := m.ctrl.Call(m, "Open", varargs...)
	ret0, _ := ret[0].(*gorm.DB)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (mr *MockDBFactoryMockRecorder) Open(dsn any, opts ...any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	varargs := append([]any{dsn}, opts...)
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Open", reflect.TypeOf((*MockDBFactory)(nil).Open), varargs...)
}
