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
