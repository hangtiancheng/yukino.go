package pool

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrencyBoundAndDrainingClose(t *testing.T) {
	p := NewGoWorkerPool(3)
	var active, maxActive, finished atomic.Int64
	var submitters sync.WaitGroup
	for i := 0; i < 50; i++ {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			if err := p.Submit(func() {
				n := active.Add(1)
				for old := maxActive.Load(); n > old && !maxActive.CompareAndSwap(old, n); old = maxActive.Load() {
				}
				active.Add(-1)
				finished.Add(1)
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	submitters.Wait()
	p.Close()
	if maxActive.Load() > 3 || finished.Load() != 50 {
		t.Fatalf("active=%d finished=%d", maxActive.Load(), finished.Load())
	}
	if err := p.Submit(func() {}); err != ErrClosed {
		t.Fatalf("submit after close: %v", err)
	}
}

func TestPanicDoesNotLoseWorker(t *testing.T) {
	p := NewGoWorkerPool(1)
	p.Submit(func() { panic("fault") })
	var done atomic.Bool
	p.Submit(func() { done.Store(true) })
	p.Close()
	if !done.Load() {
		t.Fatal("worker was lost")
	}
}
