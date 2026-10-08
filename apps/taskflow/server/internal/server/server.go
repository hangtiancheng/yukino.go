// Package server assembles the yukino_http application: middleware chain,
// public API under /api/v1 and internal callbacks under /internal/v1.
package server

import (
	"context"
	"os"
	"sync"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/consensus"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/engine"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/journal"
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

type Service struct {
	cfg           *conf.Config
	dao           *dao.DAO
	defs          *engine.DefinitionCache
	dispatcher    *engine.Dispatcher
	condition     *engine.ConditionPipeline
	monitor       *engine.Monitor
	registry      *engine.SingletonRegistry
	reports       *engine.ReportStore
	journal       *journal.Store
	ledger        *consensus.Ledger
	ready         func(context.Context) error
	storageStatus func(context.Context) map[string]any

	telemetryLog   *os.File
	telemetryLogMu sync.Mutex
}

type Deps struct {
	Cfg           *conf.Config
	DAO           *dao.DAO
	Defs          *engine.DefinitionCache
	Dispatcher    *engine.Dispatcher
	Condition     *engine.ConditionPipeline
	Monitor       *engine.Monitor
	Registry      *engine.SingletonRegistry
	Reports       *engine.ReportStore
	Journal       *journal.Store
	Ledger        *consensus.Ledger
	Ready         func(context.Context) error
	StorageStatus func(context.Context) map[string]any
}

func NewService(deps Deps) (*Service, error) {
	telemetryLog, err := openTelemetryLog(deps.Cfg.Telemetry.ClientLog())
	if err != nil {
		return nil, err
	}
	return &Service{
		cfg:           deps.Cfg,
		dao:           deps.DAO,
		defs:          deps.Defs,
		dispatcher:    deps.Dispatcher,
		condition:     deps.Condition,
		monitor:       deps.Monitor,
		registry:      deps.Registry,
		reports:       deps.Reports,
		journal:       deps.Journal,
		ledger:        deps.Ledger,
		ready:         deps.Ready,
		storageStatus: deps.StorageStatus,
		telemetryLog:  telemetryLog,
	}, nil
}

func (s *Service) Build() *yukino.Application {
	app := yukino.New()
	app.Use(
		CORSMiddleware(s.cfg.Server.CORSAllowAll, s.cfg.Server.CORSAllowedOrigins),
		TraceMiddleware(),
		SentryRecoverMiddleware(),
		AccessLogMiddleware(),
		BodyLimitMiddleware(),
	)

	api := app.Router("/api/v1")
	api.Use(func(ctx *yukino.Context, next func()) {
		if ctx.Path == "/api/v1/health" || ctx.Path == "/api/v1/ready" || ctx.Path == "/api/v1/telemetry/log" {
			next()
			return
		}
		TokenMiddleware(s.cfg.Server.APIToken(), false)(ctx, next)
	})
	api.Get("/health", s.handleHealth)
	api.Get("/ready", s.handleReady)
	api.Get("/overview", s.handleOverview)

	scheduled := api.Router("/scheduled-tasks")
	scheduled.Use(TokenMiddleware(s.cfg.Server.APIToken(), false))
	scheduled.Get("", s.handleListScheduled)
	scheduled.Post("", s.handleCreateScheduled)
	scheduled.Get("/:id", s.handleGetScheduled)
	scheduled.Patch("/:id", s.handleUpdateScheduled)
	scheduled.Delete("/:id", s.handleDeleteScheduled)
	scheduled.Post("/:id/trigger", s.handleTriggerScheduled)

	condition := api.Router("/condition-tasks")
	condition.Use(TokenMiddleware(s.cfg.Server.APIToken(), false))
	condition.Get("", s.handleListCondition)
	condition.Post("", s.handleCreateCondition)
	condition.Get("/:id", s.handleGetCondition)
	condition.Patch("/:id", s.handleUpdateCondition)
	condition.Delete("/:id", s.handleDeleteCondition)
	condition.Post("/:id/test", s.handleTestCondition)

	executions := api.Router("/executions")
	executions.Use(TokenMiddleware(s.cfg.Server.APIToken(), false))
	executions.Get("", s.handleListExecutions)
	executions.Get("/:id", s.handleGetExecution)
	executions.Get("/:id/report", s.handleGetReport)
	executions.Post("/:id/cancel", s.handleCancelExecution)

	records := api.Router("/risk-records")
	records.Use(TokenMiddleware(s.cfg.Server.APIToken(), false))
	records.Get("", s.handleListRiskRecords)
	records.Post("", s.handleCreateRiskRecord)

	monitor := api.Router("/monitor")
	monitor.Use(TokenMiddleware(s.cfg.Server.APIToken(), false))
	monitor.Get("/nodes", s.handleMonitorNodes)
	monitor.Get("/stats", s.handleMonitorStats)
	monitor.Get("/cache", s.handleMonitorCache)
	monitor.Get("/dead-letters", s.handleListDeadLetters)
	monitor.Get("/consensus", s.handleMonitorConsensus)
	monitor.Get("/journal", s.handleMonitorJournal)
	monitor.Get("/mirror", s.handleMonitorMirror)
	monitor.Get("/storage", func(ctx *yukino.Context, _ func()) {
		if s.storageStatus == nil {
			ok(ctx, yukino.H{})
			return
		}
		ok(ctx, s.storageStatus(ctx.Request.Context()))
	})

	api.Post("/telemetry/log", s.handleTelemetryLog)

	internal := app.Router("/internal/v1")
	internal.Use(TokenMiddleware(s.cfg.Server.InternalToken(), true))
	internal.Post("/fire", s.handleFire)
	internal.Get("/ping", s.handleHealth)

	return app
}
