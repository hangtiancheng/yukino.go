package breaker

import (
	"testing"
	"time"
)

func TestCircuitBreakerClosedAndOpen(t *testing.T) {
	cb := NewCircuitBreaker(4, 0.5, 20*time.Millisecond)
	for range 2 {
		cb.RecordSuccess()
	}
	for range 2 {
		cb.RecordFailure()
	}
	if cb.State() != Open {
		t.Fatalf("state = %v, want Open", cb.State())
	}
	if cb.Allow() {
		t.Fatal("open breaker should reject before timeout")
	}
	time.Sleep(25 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("open breaker should allow one half-open probe after timeout")
	}
	if cb.Allow() {
		t.Fatal("half-open breaker should allow only one probe")
	}
	cb.RecordSuccess()
	if cb.State() != Closed {
		t.Fatalf("state = %v, want Closed", cb.State())
	}
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(1, 1.0, time.Millisecond)
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatalf("state = %v, want Open", cb.State())
	}
	time.Sleep(2 * time.Millisecond)
	if !cb.Allow() {
		t.Fatal("expected half-open probe")
	}
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatalf("state = %v, want Open after failed probe", cb.State())
	}
}

func TestCircuitBreakerWindowReset(t *testing.T) {
	cb := NewCircuitBreaker(2, 1.0, time.Second)
	cb.RecordFailure()
	cb.RecordSuccess()
	if cb.State() != Closed {
		t.Fatalf("state = %v, want Closed", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != Closed {
		t.Fatalf("state = %v, want Closed before full window", cb.State())
	}
	cb.RecordFailure()
	if cb.State() != Open {
		t.Fatalf("state = %v, want Open", cb.State())
	}
}
