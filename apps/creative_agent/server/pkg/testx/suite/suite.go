// Package suite provides a lightweight suite runner built on the standard-library testing package,
// replacing testify/suite: embed suite.Suite, implement Test* methods, and run with suite.Run.
//
// Compatible with common testify/suite usage:
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

// TestingSuite is the minimal interface every suite must satisfy.
type TestingSuite interface {
	T() *testing.T
	SetT(*testing.T)
}

// Suite is the embeddable base suite struct.
type Suite struct {
	t *testing.T
}

// T returns the *testing.T of the current test.
func (s *Suite) T() *testing.T { return s.t }

// SetT is called by Run to bind the *testing.T of the current test.
func (s *Suite) SetT(t *testing.T) { s.t = t }

// Assert returns the assertion helper bound to the current test.
func (s *Suite) Assert() *Assertions { return &Assertions{t: s.t} }

// Require returns the assertion helper bound to the current test (equivalent to Assert; both are fail-fast).
func (s *Suite) Require() *Assertions { return &Assertions{t: s.t} }

// Assertions binds the testx assertions to a concrete *testing.T.
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

// Run discovers all Test* methods on the suite and executes them one by one.
// SetupSuite/TearDownSuite run once each; SetupTest/TearDownTest run before/after each test.
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
