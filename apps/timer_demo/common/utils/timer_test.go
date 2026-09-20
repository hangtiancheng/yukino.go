package utils

import (
	"testing"
	"time"
)

func TestUnionAndSplitTimerIDUnix(t *testing.T) {
	str := UnionTimerIDUnix(42, 1725945600000)
	timerID, unix, err := SplitTimerIDUnix(str)
	if err != nil {
		t.Fatalf("SplitTimerIDUnix(%q) returned err: %v", str, err)
	}
	if timerID != 42 || unix != 1725945600000 {
		t.Fatalf("SplitTimerIDUnix(%q) = (%d, %d), want (42, 1725945600000)", str, timerID, unix)
	}
}

func TestSplitTimerIDUnixInvalidInput(t *testing.T) {
	for _, str := range []string{"", "1", "1_2_3", "a_b", "1_x", "_100", "1_", "-1_100"} {
		timerID, unix, err := SplitTimerIDUnix(str)
		if err == nil {
			t.Errorf("SplitTimerIDUnix(%q) = (%d, %d, nil), want error", str, timerID, unix)
		}
	}
}

func TestSplitTimeBucket(t *testing.T) {
	key := "2026-09-10 10:05_3"
	got, bucket, err := SplitTimeBucket(key)
	if err != nil {
		t.Fatalf("SplitTimeBucket(%q) returned err: %v", key, err)
	}
	want := time.Date(2026, 9, 10, 10, 5, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("SplitTimeBucket(%q) time = %v, want %v", key, got, want)
	}
	if bucket != 3 {
		t.Fatalf("SplitTimeBucket(%q) bucket = %d, want 3", key, bucket)
	}
}

func TestSplitTimeBucketInvalidInput(t *testing.T) {
	if _, _, err := SplitTimeBucket("no-underscore-here"); err == nil {
		t.Error("SplitTimeBucket with missing underscore returned nil error, want error")
	}
	if _, _, err := SplitTimeBucket("2026-09-10 10:05_x"); err == nil {
		t.Error("SplitTimeBucket with non-numeric bucket returned nil error, want error")
	}
}
