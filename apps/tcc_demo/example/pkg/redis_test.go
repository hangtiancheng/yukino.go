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

package pkg

import (
	"reflect"
	"testing"
)

func Test_GetRedisClient(t *testing.T) {
	if reflect.TypeOf(NewRedisClient("", "", "")) != reflect.TypeOf(GetRedisClient()) {
		t.Error("type mismatch")
	}
}

func Test_BuildKey(t *testing.T) {
	if got := BuildTXKey("component", "tx"); got != "txKey:component:tx" {
		t.Errorf("expected txKey:component:tx, got %s", got)
	}
	if got := BuildTXDetailKey("component", "tx"); got != "txDetailKey:component:tx" {
		t.Errorf("expected txDetailKey:component:tx, got %s", got)
	}
	if got := BuildDataKey("component", "tx", "biz"); got != "txKey:component:tx:biz" {
		t.Errorf("expected txKey:component:tx:biz, got %s", got)
	}
	if got := BuildTXLockKey("component", "tx"); got != "txLockKey:component:tx" {
		t.Errorf("expected txLockKey:component:tx, got %s", got)
	}
	if got := BuildTXRecordLockKey(); got != "tcc_demo:txRecord:lock" {
		t.Errorf("expected tcc_demo:txRecord:lock, got %s", got)
	}
}
