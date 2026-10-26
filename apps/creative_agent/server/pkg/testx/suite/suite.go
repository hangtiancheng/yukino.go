// Package suite 提供基于标准库 testing 的轻量 suite 运行器，
// 取代 testify/suite：嵌入 suite.Suite、实现 Test* 方法、suite.Run 执行。
//
// 兼容 testify/suite 的常见用法：
//
//	type MySuite struct {
//		suite.Suite
//	}
//
//	func (s *MySuite) SetupSuite()    {}
//	func (s *MySuite) SetupTest()     {}
//	func (s *MySuite) TestSomething() { testx.Equal(s.T(), 1, 1) }
//
//	func TestMySuite(t *testing.T) { suite.Run(t, new(MySuite)) }
package suite

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestingSuite 是所有 suite 必须满足的最小接口。
type TestingSuite interface {
	T() *testing.T
	SetT(*testing.T)
}

// Suite 是可嵌入的基础 suite 结构。
type Suite struct {
	t *testing.T
}

// T 返回当前测试的 *testing.T。
func (s *Suite) T() *testing.T { return s.t }

// SetT 由 Run 调用，绑定当前测试的 *testing.T。
func (s *Suite) SetT(t *testing.T) { s.t = t }

// Assert 返回绑定当前测试的断言助手。
func (s *Suite) Assert() *Assertions { return &Assertions{t: s.t} }

// Require 返回绑定当前测试的断言助手（与 Assert 等价，均为 fail-fast）。
func (s *Suite) Require() *Assertions { return &Assertions{t: s.t} }

// Assertions 将 testx 断言绑定到具体 *testing.T。
type Assertions struct {
	t *testing.T
}

func (a *Assertions) NoError(err error, msgAndArgs ...any) {
	a.t.Helper()
	testx.NoError(a.t, err, msgAndArgs...)
}

func (a *Assertions) Error(err error, msgAndArgs ...any) {
	a.t.Helper()
	testx.Error(a.t, err, msgAndArgs...)
}

func (a *Assertions) ErrorIs(err, target error, msgAndArgs ...any) {
	a.t.Helper()
	testx.ErrorIs(a.t, err, target, msgAndArgs...)
}

func (a *Assertions) Equal(expected, actual any, msgAndArgs ...any) {
	a.t.Helper()
	testx.Equal(a.t, expected, actual, msgAndArgs...)
}

func (a *Assertions) NotEqual(expected, actual any, msgAndArgs ...any) {
	a.t.Helper()
	testx.NotEqual(a.t, expected, actual, msgAndArgs...)
}

func (a *Assertions) True(value bool, msgAndArgs ...any) {
	a.t.Helper()
	testx.True(a.t, value, msgAndArgs...)
}

func (a *Assertions) False(value bool, msgAndArgs ...any) {
	a.t.Helper()
	testx.False(a.t, value, msgAndArgs...)
}

func (a *Assertions) Nil(object any, msgAndArgs ...any) {
	a.t.Helper()
	testx.Nil(a.t, object, msgAndArgs...)
}

func (a *Assertions) NotNil(object any, msgAndArgs ...any) {
	a.t.Helper()
	testx.NotNil(a.t, object, msgAndArgs...)
}

func (a *Assertions) Empty(object any, msgAndArgs ...any) {
	a.t.Helper()
	testx.Empty(a.t, object, msgAndArgs...)
}

func (a *Assertions) NotEmpty(object any, msgAndArgs ...any) {
	a.t.Helper()
	testx.NotEmpty(a.t, object, msgAndArgs...)
}

func (a *Assertions) Len(object any, expected int, msgAndArgs ...any) {
	a.t.Helper()
	testx.Len(a.t, object, expected, msgAndArgs...)
}

func (a *Assertions) Contains(container, element any, msgAndArgs ...any) {
	a.t.Helper()
	testx.Contains(a.t, container, element, msgAndArgs...)
}

// Run 发现 suite 上所有 Test* 方法并逐个执行。
// SetupSuite/TearDownSuite 各执行一次；SetupTest/TearDownTest 在每个测试前后执行。
func Run(t *testing.T, s TestingSuite) {
	t.Helper()
	s.SetT(t)

	if setup, ok := s.(interface{ SetupSuite() }); ok {
		setup.SetupSuite()
	}
	if teardown, ok := s.(interface{ TearDownSuite() }); ok {
		defer teardown.TearDownSuite()
	}

	value := reflect.ValueOf(s)
	typ := value.Type()
	for i := range typ.NumMethod() {
		method := typ.Method(i)
		if !strings.HasPrefix(method.Name, "Test") {
			continue
		}
		if method.Type.NumIn() != 1 || method.Type.NumOut() != 0 {
			continue
		}
		m := method
		t.Run(m.Name, func(t *testing.T) {
			s.SetT(t)
			if setup, ok := s.(interface{ SetupTest() }); ok {
				setup.SetupTest()
			}
			if teardown, ok := s.(interface{ TearDownTest() }); ok {
				defer teardown.TearDownTest()
			}
			m.Func.Call([]reflect.Value{reflect.ValueOf(s)})
		})
	}
}
