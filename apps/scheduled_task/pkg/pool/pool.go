package pool

import (
	"errors"
	"log/slog"
	"sync"
)

type WorkerPool interface{ Submit(func()) error }

var ErrClosed = errors.New("worker pool is closed")

type GoWorkerPool struct {
	queue   chan func()
	closing chan struct{}
	mu      sync.RWMutex
	once    sync.Once
	wg      sync.WaitGroup
}

func NewGoWorkerPool(size int) *GoWorkerPool {
	if size < 1 {
		size = 1
	}
	p := &GoWorkerPool{queue: make(chan func(), size*4), closing: make(chan struct{})}
	p.wg.Add(size)
	for i := 0; i < size; i++ {
		go p.run()
	}
	return p
}

func (p *GoWorkerPool) Submit(fn func()) error {
	if fn == nil {
		return errors.New("nil worker task")
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	select {
	case <-p.closing:
		return ErrClosed
	default:
	}
	select {
	case <-p.closing:
		return ErrClosed
	case p.queue <- fn:
		return nil
	}
}

func (p *GoWorkerPool) run() {
	defer p.wg.Done()
	for fn := range p.queue {
		func() {
			defer func() {
				if fault := recover(); fault != nil {
					slog.Error("worker task panicked", "panic", fault)
				}
			}()
			fn()
		}()
	}
}

func (p *GoWorkerPool) Close() {
	p.once.Do(func() { close(p.closing); p.mu.Lock(); close(p.queue); p.mu.Unlock() })
	p.wg.Wait()
}
