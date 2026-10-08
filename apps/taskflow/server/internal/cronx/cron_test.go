package cronx

import (
	"testing"
	"time"
)

var loc = time.FixedZone("CST", 8*3600)

func TestParseErrors(t *testing.T) {
	bad := []string{
		"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *",
		"* * * 13 *", "* * * * 7", "* * * * 8", "a * * * *",
		"*/0 * * * *", "5-1 * * * *",
	}
	for _, expr := range bad {
		if _, err := Parse(expr); err == nil {
			t.Fatalf("expected error for %q", expr)
		}
	}
}

func TestDailyAt1000(t *testing.T) {
	sched, err := Parse("0 10 * * *")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 10, 5, 9, 59, 0, 0, loc)
	next := sched.Next(base)
	want := time.Date(2026, 10, 5, 10, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}

	atFire := sched.Next(want)
	want2 := time.Date(2026, 10, 6, 10, 0, 0, 0, loc)
	if !atFire.Equal(want2) {
		t.Fatalf("next after fire = %v, want %v", atFire, want2)
	}
}

func TestStepAndRange(t *testing.T) {
	sched, err := Parse("*/15 9-11 * * *")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 5, 9, 16, 0, 0, loc)
	next := sched.Next(base)
	want := time.Date(2026, 10, 5, 9, 30, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

func TestDayOfWeekSunday(t *testing.T) {
	sched, err := Parse("30 8 * * 0")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-10-05 is a Monday.
	base := time.Date(2026, 10, 5, 0, 0, 0, 0, loc)
	next := sched.Next(base)
	want := time.Date(2026, 10, 11, 8, 30, 0, 0, loc) // Sunday
	if !next.Equal(want) {
		t.Fatalf("next = %v, want %v", next, want)
	}
}

func TestNextN(t *testing.T) {
	sched, _ := Parse("0 10 * * *")
	base := time.Date(2026, 10, 5, 10, 0, 0, 0, loc)
	fires := sched.NextN(base, 3)
	if len(fires) != 3 {
		t.Fatalf("got %d fires", len(fires))
	}
	for i, wantDay := range []int{6, 7, 8} {
		want := time.Date(2026, 10, wantDay, 10, 0, 0, 0, loc)
		if !fires[i].Equal(want) {
			t.Fatalf("fire[%d] = %v, want %v", i, fires[i], want)
		}
	}
}
