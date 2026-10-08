package monitor

import (
	"context"
	"sync"

	service "github.com/hangtiancheng/yukino.go/components/timer/internal/service/monitor"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/log"
)

type MonitorApp struct {
	sync.Once
	ctx    context.Context
	stop   func()
	worker *service.Worker
}

func NewMonitorApp(worker *service.Worker) *MonitorApp {
	m := MonitorApp{
		worker: worker,
	}

	m.ctx, m.stop = context.WithCancel(context.Background())
	return &m
}

func (m *MonitorApp) Start() {
	m.Do(func() {
		log.InfoContext(m.ctx, "monitor is starting")
		go m.worker.Start(m.ctx)
	})
}

func (m *MonitorApp) Stop() {
	m.stop()
}
