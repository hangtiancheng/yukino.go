package runtime

import (
	"strconv"
	"testing"
)

func TestGetCurrentGoroutineID(t *testing.T) {
	id := GetCurrentGoroutineID()
	if id == "" {
		t.Fatal("GetCurrentGoroutineID returned an empty string")
	}
	if _, err := strconv.Atoi(id); err != nil {
		t.Fatalf("goroutine id %q is not numeric: %v", id, err)
	}
	if again := GetCurrentGoroutineID(); again != id {
		t.Fatalf("id changed within one goroutine: %q vs %q", id, again)
	}
}

func TestGetCurrentProcessAndGoroutineIDStr(t *testing.T) {
	got := GetCurrentProcessAndGoroutineIDStr()
	wantPrefix := strconv.Itoa(GetCurrentProcessID()) + "_"
	if len(got) <= len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("id = %q, want prefix %q", got, wantPrefix)
	}
}
