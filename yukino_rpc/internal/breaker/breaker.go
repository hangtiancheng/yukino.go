package breaker

import (
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

type CircuitBreaker struct {
	mu sync.Mutex

	state State

	failureCount int
	successCount int

	windowSize       int
	failureThreshold float64
	openTimeout      time.Duration

	lastStateChange time.Time
	halfOpenProbe   bool
}

func NewCircuitBreaker(windowSize int, failureThreshold float64, openTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            Closed,
		windowSize:       windowSize,
		failureThreshold: failureThreshold,
		openTimeout:      openTimeout,
		lastStateChange:  time.Now(),
	}
}
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {

	case Closed:
		return true

	case Open:
		if time.Since(cb.lastStateChange) > cb.openTimeout {
			cb.state = HalfOpen
			cb.halfOpenProbe = true
			return true
		}
		return false

	case HalfOpen:
		if cb.halfOpenProbe {
			return false
		}
		cb.halfOpenProbe = true
		return true
	}

	return true
}
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {

	case Closed:
		cb.successCount++
		cb.resetClosedWindowIfReady()

	case HalfOpen:
		cb.toClosed()

	case Open:
	}
}
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {

	case Closed:
		cb.failureCount++

		total := cb.failureCount + cb.successCount
		if total < cb.windowSize {
			return
		}

		rate := float64(cb.failureCount) / float64(total)
		if rate >= cb.failureThreshold {
			cb.toOpen()
			return
		}

		cb.resetCounts()

	case HalfOpen:
		cb.toOpen()

	case Open:
	}
}
func (cb *CircuitBreaker) toOpen() {
	cb.state = Open
	cb.lastStateChange = time.Now()
	cb.resetCounts()
	cb.halfOpenProbe = false
}

func (cb *CircuitBreaker) toClosed() {
	cb.state = Closed
	cb.lastStateChange = time.Now()
	cb.resetCounts()
	cb.halfOpenProbe = false
}
func (cb *CircuitBreaker) resetCounts() {
	cb.failureCount = 0
	cb.successCount = 0
}

func (cb *CircuitBreaker) resetClosedWindowIfReady() {
	total := cb.failureCount + cb.successCount
	if total < cb.windowSize {
		return
	}
	rate := float64(cb.failureCount) / float64(total)
	if rate >= cb.failureThreshold {
		cb.toOpen()
		return
	}
	cb.resetCounts()
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
