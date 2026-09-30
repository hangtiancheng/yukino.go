package cron

import (
	"testing"
	"time"
)

func TestIsValidCronExpr(t *testing.T) {
	p := NewCronParser()

	valid := []string{
		"* * * * *",
		"*/5 * * * *",
		"0 0 1 1 *",
		"30 4 1,15 * 5",
		"1-30/2 * * * *",
		"5,10,15 * * * *",
	}
	for _, expr := range valid {
		if !p.IsValidCronExpr(expr) {
			t.Errorf("IsValidCronExpr(%q) = false, want true", expr)
		}
	}

	invalid := []string{
		"",
		"* * * *",
		"* * * * * *",
		"60 * * * *",
		"* 24 * * *",
		"* * 0 * *",
		"* * * 13 *",
		"* * * * 7",
		"*/0 * * * *",
		"a * * * *",
		"1-5 * * * 8",
	}
	for _, expr := range invalid {
		if p.IsValidCronExpr(expr) {
			t.Errorf("IsValidCronExpr(%q) = true, want false", expr)
		}
	}
}

func TestNextAfter(t *testing.T) {
	p := NewCronParser()

	after := time.Date(2026, 9, 10, 10, 3, 30, 0, time.Local)
	next, err := p.NextAfter("*/5 * * * *", after)
	if err != nil {
		t.Fatalf("NextAfter returned err: %v", err)
	}
	want := time.Date(2026, 9, 10, 10, 5, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("NextAfter = %v, want %v", next, want)
	}
}

func TestNextAfterIsStrictlyAfter(t *testing.T) {
	p := NewCronParser()

	// Querying exactly at a fire time must return the next fire, not the
	// current one.
	at := time.Date(2026, 9, 10, 10, 5, 0, 0, time.Local)
	next, err := p.NextAfter("*/5 * * * *", at)
	if err != nil {
		t.Fatalf("NextAfter returned err: %v", err)
	}
	want := time.Date(2026, 9, 10, 10, 10, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("NextAfter = %v, want %v", next, want)
	}
}

func TestNextsBetween(t *testing.T) {
	p := NewCronParser()

	// The window excludes both endpoints: [start, end).
	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, 9, 10, 2, 30, 0, 0, time.Local)
	nexts, err := p.NextsBetween("0 * * * *", start, end)
	if err != nil {
		t.Fatalf("NextsBetween returned err: %v", err)
	}
	want := []time.Time{
		time.Date(2026, 9, 10, 1, 0, 0, 0, time.Local),
		time.Date(2026, 9, 10, 2, 0, 0, 0, time.Local),
	}
	if len(nexts) != len(want) {
		t.Fatalf("NextsBetween = %v, want %v", nexts, want)
	}
	for i := range want {
		if !nexts[i].Equal(want[i]) {
			t.Fatalf("NextsBetween[%d] = %v, want %v", i, nexts[i], want[i])
		}
	}

	// end == start must yield no fire times.
	if nexts, err := p.NextsBetween("* * * * *", start, start); err != nil || len(nexts) != 0 {
		t.Fatalf("NextsBetween with end == start = (%v, %v), want (empty, nil)", nexts, err)
	}
}

// TestNextsBetweenInsideScheduledMinute guards against the regression where a
// query landing inside a scheduled minute matched on every following second
// (e.g. 10:05:31..10:05:59 for "*/5 * * * *" queried at 10:05:30).
func TestNextsBetweenInsideScheduledMinute(t *testing.T) {
	p := NewCronParser()

	start := time.Date(2026, 9, 10, 10, 5, 30, 0, time.Local)
	end := time.Date(2026, 9, 10, 10, 20, 0, 0, time.Local)
	nexts, err := p.NextsBetween("*/5 * * * *", start, end)
	if err != nil {
		t.Fatalf("NextsBetween returned err: %v", err)
	}
	if len(nexts) != 2 {
		t.Fatalf("NextsBetween returned %d entries (%v), want 2", len(nexts), nexts)
	}
	for _, want := range []time.Time{
		time.Date(2026, 9, 10, 10, 10, 0, 0, time.Local),
		time.Date(2026, 9, 10, 10, 15, 0, 0, time.Local),
	} {
		found := false
		for _, got := range nexts {
			if got.Equal(want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("NextsBetween result %v misses %v", nexts, want)
		}
	}
}

func TestNextsBetweenEndBeforeStart(t *testing.T) {
	p := NewCronParser()

	start := time.Date(2026, 9, 10, 10, 0, 0, 0, time.Local)
	end := time.Date(2026, 9, 10, 9, 0, 0, 0, time.Local)
	if _, err := p.NextsBetween("* * * * *", start, end); err == nil {
		t.Fatal("NextsBetween with end before start returned nil error, want error")
	}
}

func TestNextDayOfWeekAndMonth(t *testing.T) {
	p := NewCronParser()

	// 2026-09-10 is a Thursday; the next Friday is 2026-09-11.
	after := time.Date(2026, 9, 10, 12, 0, 0, 0, time.Local)
	next, err := p.NextAfter("0 8 * * 5", after)
	if err != nil {
		t.Fatalf("NextAfter returned err: %v", err)
	}
	want := time.Date(2026, 9, 11, 8, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("NextAfter = %v (%s), want %v", next, next.Weekday(), want)
	}

	// Only February: the next fire must land in February 2027.
	next, err = p.NextAfter("0 0 1 2 *", after)
	if err != nil {
		t.Fatalf("NextAfter returned err: %v", err)
	}
	want = time.Date(2027, 2, 1, 0, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("NextAfter = %v, want %v", next, want)
	}
}

func TestNextNeverMatchingExprBounded(t *testing.T) {
	p := NewCronParser()

	// Feb 31 never exists; Next must give up instead of looping forever.
	after := time.Date(2026, 9, 10, 12, 0, 0, 0, time.Local)
	done := make(chan struct{})
	var next time.Time
	var err error
	go func() {
		next, err = p.NextAfter("0 0 31 2 *", after)
		close(done)
	}()

	select {
	case <-done:
		if !next.IsZero() || err == nil {
			t.Fatalf("NextAfter = %v, err = %v; want zero time and error", next, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("NextAfter did not terminate for a never-matching cron expression")
	}
}
