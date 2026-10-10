package migrator

import (
	"context"
	"sync"

	service "github.com/hangtiancheng/yukino.go/components/timer/internal/service/migrator"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/log"
)

type MigratorApp struct {
	sync.Once
	ctx    context.Context
	stop   func()
	worker *service.Worker
}

func NewMigratorApp(worker *service.Worker) *MigratorApp {
	m := MigratorApp{
		worker: worker,
	}

	m.ctx, m.stop = context.WithCancel(context.Background())
	return &m
}

func (m *MigratorApp) Start() {
	m.Do(func() {
		log.InfoContext(m.ctx, "migrator is starting")
		go func() {
			if err := m.worker.Start(m.ctx); err != nil {
				log.ErrorContextf(m.ctx, "start worker failed, err: %v", err)
			}
		}()
	})
}

func (m *MigratorApp) Stop() {
	m.stop()
}
