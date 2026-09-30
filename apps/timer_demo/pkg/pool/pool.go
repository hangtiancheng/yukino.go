package pool

import (
	"github.com/bytedance/gopkg/util/gopool"
)

// WorkerPool is the interface for goroutine worker pools.
type WorkerPool interface {
	Submit(func()) error
}

// GoWorkerPool is a goroutine worker pool backed by gopool.
type GoWorkerPool struct {
	pool gopool.Pool
}

// Submit submits a task to the pool.
func (g *GoWorkerPool) Submit(f func()) error {
	g.pool.Go(f)
	return nil
}

func NewGoWorkerPool(size int) *GoWorkerPool {
	return &GoWorkerPool{
		pool: gopool.NewPool("timer_demo", int32(size), nil),
	}
}
