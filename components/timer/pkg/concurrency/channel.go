package concurrency

import (
	"context"
	"sync"
)

type SafeChan struct {
	sync.Once
	ctx   context.Context
	close func()
	ch    chan any
	mu    sync.RWMutex
}

func NewSafeChan(size int) *SafeChan {
	s := SafeChan{
		ch: make(chan any, size),
	}
	s.ctx, s.close = context.WithCancel(context.Background())
	return &s
}

func (s *SafeChan) Put(element any) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	select {
	case <-s.ctx.Done():
		return
	default:
	}

	select {
	case s.ch <- element:
	default:
	}
}

func (s *SafeChan) GetChan() chan any {
	return s.ch
}

func (s *SafeChan) Get() any {
	return <-s.ch
}

func (s *SafeChan) Close() {
	s.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.close()
		close(s.ch)
	})
}
