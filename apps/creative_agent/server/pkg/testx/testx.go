// Package testx provides lightweight assertion helpers built on the standard-library testing package,
// replacing testify's assert/require (all fail-fast: a failed assertion immediately calls t.Fatalf).
package testx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// AnError is a generic error for tests (aligned with testify's assert.AnError).
var AnError = errors.New("testx.AnError general error for testing")

func message(msgAndArgs []any) string {
	if len(msgAndArgs) == 0 {
		return ""
	}
	if len(msgAndArgs) == 1 {
		if s, ok := msgAndArgs[0].(string); ok {
			return ": " + s
		}
		return ": " + fmt.Sprint(msgAndArgs[0])
	}
	if format, ok := msgAndArgs[0].(string); ok {
		return ": " + fmt.Sprintf(format, msgAndArgs[1:]...)
	}
	return ": " + fmt.Sprint(msgAndArgs...)
}

func fail(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}

func describe(v any) string {
	return fmt.Sprintf("%#v (%T)", v, v)
}

// NoError asserts that err is nil.
func NoError(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	if err != nil {
		fail(t, "unexpected error: %v%s", err, message(msgAndArgs))
	}
}

// Error asserts that err is non-nil.
func Error(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	if err == nil {
		fail(t, "expected an error but got nil%s", message(msgAndArgs))
	}
}

// ErrorIs asserts that errors.Is(err, target) holds.
func ErrorIs(t *testing.T, err error, target error, msgAndArgs ...any) {
	t.Helper()
	if !errors.Is(err, target) {
		fail(t, "error %v does not match target %v%s", err, target, message(msgAndArgs))
	}
}

// Equal asserts that expected and actual are deeply equal.
func Equal(t *testing.T, expected, actual any, msgAndArgs ...any) {
	t.Helper()
	if !objectsAreEqual(expected, actual) {
		fail(t, "not equal:\n  expected: %s\n  actual:   %s%s",
			describe(expected), describe(actual), message(msgAndArgs))
	}
}

// NotEqual asserts that expected and actual are not equal.
func NotEqual(t *testing.T, expected, actual any, msgAndArgs ...any) {
	t.Helper()
	if objectsAreEqual(expected, actual) {
		fail(t, "values should not be equal: %s%s", describe(actual), message(msgAndArgs))
	}
}

func objectsAreEqual(expected, actual any) bool {
	if expected == nil || actual == nil {
		return expected == actual
	}
	exp, ok := expected.([]byte)
	if !ok {
		return reflect.DeepEqual(expected, actual)
	}
	act, ok := actual.([]byte)
	if !ok {
		return false
	}
	return bytes.Equal(exp, act)
}

// True asserts that the condition is true.
func True(t *testing.T, value bool, msgAndArgs ...any) {
	t.Helper()
	if !value {
		fail(t, "expected true%s", message(msgAndArgs))
	}
}

// False asserts that the condition is false.
func False(t *testing.T, value bool, msgAndArgs ...any) {
	t.Helper()
	if value {
		fail(t, "expected false%s", message(msgAndArgs))
	}
}

// Nil asserts that the object is nil (including typed nil).
func Nil(t *testing.T, object any, msgAndArgs ...any) {
	t.Helper()
	if !isNil(object) {
		fail(t, "expected nil, got %s%s", describe(object), message(msgAndArgs))
	}
}

// NotNil asserts that the object is non-nil.
func NotNil(t *testing.T, object any, msgAndArgs ...any) {
	t.Helper()
	if isNil(object) {
		fail(t, "expected non-nil value%s", message(msgAndArgs))
	}
}

func isNil(object any) bool {
	if object == nil {
		return true
	}
	value := reflect.ValueOf(object)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface,
		reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	}
	return false
}

// Empty asserts that the object is empty (nil, zero-length collection, empty string, zero value).
func Empty(t *testing.T, object any, msgAndArgs ...any) {
	t.Helper()
	if !isZeroLike(object) {
		fail(t, "expected empty, got %s%s", describe(object), message(msgAndArgs))
	}
}

// NotEmpty asserts that the object is non-empty.
func NotEmpty(t *testing.T, object any, msgAndArgs ...any) {
	t.Helper()
	if isZeroLike(object) {
		fail(t, "expected non-empty value%s", message(msgAndArgs))
	}
}

// Zero asserts that the object is the zero value.
func Zero(t *testing.T, object any, msgAndArgs ...any) {
	t.Helper()
	if !isZeroLike(object) {
		fail(t, "expected zero value, got %s%s", describe(object), message(msgAndArgs))
	}
}

// NotZero asserts that the object is not the zero value.
func NotZero(t *testing.T, object any, msgAndArgs ...any) {
	t.Helper()
	if isZeroLike(object) {
		fail(t, "expected non-zero value%s", message(msgAndArgs))
	}
}

func isZeroLike(object any) bool {
	if object == nil {
		return true
	}
	value := reflect.ValueOf(object)
	switch value.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool:
		return !value.Bool()
	case reflect.Pointer, reflect.Interface:
		if value.IsNil() {
			return true
		}
		return isZeroLike(value.Elem().Interface())
	}
	zero := reflect.Zero(value.Type()).Interface()
	return reflect.DeepEqual(object, zero)
}

// Len asserts that the object's length equals expected.
func Len(t *testing.T, object any, expected int, msgAndArgs ...any) {
	t.Helper()
	length, ok := lengthOf(object)
	if !ok {
		fail(t, "value of type %T has no length%s", object, message(msgAndArgs))
	}
	if length != expected {
		fail(t, "expected length %d, got %d%s", expected, length, message(msgAndArgs))
	}
}

func lengthOf(object any) (int, bool) {
	if object == nil {
		return 0, false
	}
	value := reflect.ValueOf(object)
	switch value.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return value.Len(), true
	}
	return 0, false
}

// Contains asserts that the container holds the element, or the string contains the substring.
func Contains(t *testing.T, container, element any, msgAndArgs ...any) {
	t.Helper()
	if !containsElement(container, element) {
		fail(t, "%s does not contain %s%s",
			describe(container), describe(element), message(msgAndArgs))
	}
}

// NotContains asserts that the container does not hold the element, or the string does not contain the substring.
func NotContains(t *testing.T, container, element any, msgAndArgs ...any) {
	t.Helper()
	if containsElement(container, element) {
		fail(t, "%s should not contain %s%s",
			describe(container), describe(element), message(msgAndArgs))
	}
}

func containsElement(container, element any) bool {
	if container == nil {
		return false
	}
	if s, ok := container.(string); ok {
		sub, ok := element.(string)
		if !ok {
			return false
		}
		return strings.Contains(s, sub)
	}
	value := reflect.ValueOf(container)
	switch value.Kind() {
	case reflect.Map:
		key := reflect.ValueOf(element)
		if key.Type().AssignableTo(value.Type().Key()) {
			return value.MapIndex(key).IsValid()
		}
		return false
	case reflect.Slice, reflect.Array:
		for i := range value.Len() {
			if objectsAreEqual(value.Index(i).Interface(), element) {
				return true
			}
		}
	}
	return false
}

// ElementsMatch asserts that two lists contain the same elements (ignoring order and duplicate positions).
func ElementsMatch(t *testing.T, listA, listB any, msgAndArgs ...any) {
	t.Helper()
	a := reflect.ValueOf(listA)
	b := reflect.ValueOf(listB)
	if a.Kind() != reflect.Slice && a.Kind() != reflect.Array {
		fail(t, "listA of type %T is not a slice%s", listA, message(msgAndArgs))
	}
	if b.Kind() != reflect.Slice && b.Kind() != reflect.Array {
		fail(t, "listB of type %T is not a slice%s", listB, message(msgAndArgs))
	}
	if a.Len() != b.Len() {
		fail(t, "length mismatch: %d vs %d%s", a.Len(), b.Len(), message(msgAndArgs))
	}
	used := make([]bool, b.Len())
	for i := range a.Len() {
		found := false
		for j := range b.Len() {
			if !used[j] && objectsAreEqual(a.Index(i).Interface(), b.Index(j).Interface()) {
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			fail(t, "element %s not found in listB%s", describe(a.Index(i).Interface()), message(msgAndArgs))
		}
	}
}

// Greater asserts e1 > e2 (numeric or string).
func Greater(t *testing.T, e1, e2 any, msgAndArgs ...any) {
	t.Helper()
	cmp, ok := compareOrdered(e1, e2)
	if !ok {
		fail(t, "values %s and %s are not ordered-comparable%s", describe(e1), describe(e2), message(msgAndArgs))
	}
	if cmp <= 0 {
		fail(t, "expected %s > %s%s", describe(e1), describe(e2), message(msgAndArgs))
	}
}

// GreaterOrEqual asserts e1 >= e2 (numeric or string).
func GreaterOrEqual(t *testing.T, e1, e2 any, msgAndArgs ...any) {
	t.Helper()
	cmp, ok := compareOrdered(e1, e2)
	if !ok {
		fail(t, "values %s and %s are not ordered-comparable%s", describe(e1), describe(e2), message(msgAndArgs))
	}
	if cmp < 0 {
		fail(t, "expected %s >= %s%s", describe(e1), describe(e2), message(msgAndArgs))
	}
}

func compareOrdered(e1, e2 any) (int, bool) {
	if t1, ok := e1.(time.Time); ok {
		if t2, ok := e2.(time.Time); ok {
			switch {
			case t1.Before(t2):
				return -1, true
			case t1.After(t2):
				return 1, true
			default:
				return 0, true
			}
		}
		return 0, false
	}
	v1 := reflect.ValueOf(e1)
	v2 := reflect.ValueOf(e2)
	if v1.Kind() != v2.Kind() {
		return 0, false
	}
	switch v1.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		a, b := v1.Int(), v2.Int()
		return cmpResult(a, b), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		a, b := v1.Uint(), v2.Uint()
		return cmpUint(a, b), true
	case reflect.Float32, reflect.Float64:
		a, b := v1.Float(), v2.Float()
		switch {
		case a < b:
			return -1, true
		case a > b:
			return 1, true
		default:
			return 0, true
		}
	case reflect.String:
		return strings.Compare(v1.String(), v2.String()), true
	}
	return 0, false
}

func cmpResult[T int64 | uint64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpUint(a, b uint64) int { return cmpResult(a, b) }

// JSONEq asserts that two JSON strings are semantically equal.
func JSONEq(t *testing.T, expected, actual string, msgAndArgs ...any) {
	t.Helper()
	var expectedValue, actualValue any
	if err := json.Unmarshal([]byte(expected), &expectedValue); err != nil {
		fail(t, "expected value %q is not valid JSON: %v%s", expected, err, message(msgAndArgs))
	}
	if err := json.Unmarshal([]byte(actual), &actualValue); err != nil {
		fail(t, "actual value %q is not valid JSON: %v%s", actual, err, message(msgAndArgs))
	}
	if !reflect.DeepEqual(expectedValue, actualValue) {
		fail(t, "JSON not equal:\n  expected: %s\n  actual:   %s%s", expected, actual, message(msgAndArgs))
	}
}

// Eventually polls condition at the tick interval within waitFor until it becomes true.
func Eventually(t *testing.T, condition func() bool, waitFor, tick time.Duration, msgAndArgs ...any) {
	t.Helper()
	deadline := time.After(waitFor)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			fail(t, "condition never satisfied within %v%s", waitFor, message(msgAndArgs))
			return
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// NotPanics asserts that executing f does not panic.
func NotPanics(t *testing.T, f func(), msgAndArgs ...any) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			fail(t, "unexpected panic: %v%s", r, message(msgAndArgs))
		}
	}()
	f()
}
