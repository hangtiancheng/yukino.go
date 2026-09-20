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
	// mu guards ch against sends racing with close: Put holds the read lock
	// while sending, Close holds the write lock while closing the channel.
	mu sync.RWMutex
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

	// Check Done first, in its own select. Once the channel is closed, a
	// send is "ready" and panics, and select picks randomly among ready
	// cases, so the send case must not share a select with the Done case.
	// Holding the read lock guarantees Close (which takes the write lock)
	// cannot run between this check and the send below.
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
