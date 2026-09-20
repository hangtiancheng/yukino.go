// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
