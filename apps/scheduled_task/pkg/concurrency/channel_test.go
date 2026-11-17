package concurrency

import (
	"sync"
	"testing"
	"time"
)

func TestSafeChanPutAndGet(t *testing.T) {
	s := NewSafeChan(4)
	defer s.Close()

	s.Put(1)
	if v := s.Get(); v != 1 {
		t.Fatalf("Get() = %v, want 1", v)
	}
}

func TestSafeChanPutDoesNotBlockWhenFull(t *testing.T) {
	s := NewSafeChan(1)
	defer s.Close()

	s.Put(1)
	done := make(chan struct{})
	go func() {
		s.Put(2)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Put blocked on a full channel")
	}
}

func TestSafeChanCloseIsIdempotent(t *testing.T) {
	s := NewSafeChan(1)
	s.Close()
	s.Close()

	s.Put(1)
	if v := s.Get(); v != nil {
		t.Fatalf("Get() after close = %v, want nil", v)
	}
}

func TestSafeChanConcurrentPutAndClose(t *testing.T) {
	s := NewSafeChan(16)

	const putters = 8
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < putters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				s.Put(i)
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	s.Close()
	close(stop)
	wg.Wait()

	for v := range s.GetChan() {
		_ = v
	}
}

func TestSafeChanPutAfterCloseNoPanic(t *testing.T) {
	s := NewSafeChan(1)
	s.Close()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Put(j)
			}
		}()
	}
	wg.Wait()

	if v := s.Get(); v != nil {
		t.Fatalf("Get() after close = %v, want nil", v)
	}
}
