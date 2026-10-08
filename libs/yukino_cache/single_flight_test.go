package yukino_cache_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/libs/yukino_cache"
)

func TestDoReturnsValueAndError(t *testing.T) {
	var g yukino_cache.SingleFlightGroup
	value, err := g.Do("key", func() (any, error) {
		return "value", nil
	})
	if err != nil || value != "value" {
		t.Fatalf("Do returned %v, %v", value, err)
	}

	wantErr := errors.New("load failed")
	value, err = g.Do("err", func() (any, error) {
		return nil, wantErr
	})
	if value != nil || !errors.Is(err, wantErr) {
		t.Fatalf("Do error result = %v, %v", value, err)
	}
}

func TestDoSuppressesDuplicateCalls(t *testing.T) {
	var (
		g     yukino_cache.SingleFlightGroup
		calls int
		mu    sync.Mutex
	)
	ready := make(chan struct{})
	release := make(chan struct{})

	const callers = 16
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			value, err := g.Do("key", func() (any, error) {
				mu.Lock()
				calls++
				if calls == 1 {
					close(ready)
				}
				mu.Unlock()
				<-release
				return "value", nil
			})
			if err != nil {
				errs <- err
				return
			}
			if value != "value" {
				errs <- errors.New("unexpected value")
			}
		})
	}
	<-ready
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}

	if _, err := g.Do("key", func() (any, error) {
		calls++
		return "next", nil
	}); err != nil {
		t.Fatalf("sequential Do returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("sequential calls = %d, want 2", calls)
	}
}
