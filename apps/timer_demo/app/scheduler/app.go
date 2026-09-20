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

package scheduler

import (
	"context"
	"sync"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/log"
	service "github.com/hangtiancheng/yukino.go/apps/timer_demo/service/scheduler"
)

// WorkerApp reads configuration and launches multiple goroutines for scheduling.
type WorkerApp struct {
	sync.Once
	service workerService
	ctx     context.Context
	stop    func()
}

func NewWorkerApp(service *service.Worker) *WorkerApp {
	w := WorkerApp{
		service: service,
	}

	w.ctx, w.stop = context.WithCancel(context.Background())
	return &w
}

func (w *WorkerApp) Start() {
	w.Do(w.start)
}

func (w *WorkerApp) start() {
	log.InfoContext(w.ctx, "worker app is starting")
	go func() {
		if err := w.service.Start(w.ctx); err != nil {
			log.ErrorContextf(w.ctx, "worker start failed, err: %v", err)
		}
	}()
}

func (w *WorkerApp) Stop() {
	w.stop()
	log.WarnContext(w.ctx, "worker app is stopped")
}

type workerService interface {
	Start(context.Context) error
}
