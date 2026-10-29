package monitor

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/dao"
	creationdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	creationapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
	riskinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	riskdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	riskapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Scheduled-job intervals (aligned with the original project's resident monitor job cadence).
const (
	// CompensationJobInterval is the compensation-retry interval: scan every 1 minute.
	CompensationJobInterval = 1 * time.Minute
	// TimeoutJobInterval is the timeout-fallback interval: scan every 5 minutes.
	TimeoutJobInterval = 5 * time.Minute
	// TimeoutHours is the work-generation timeout threshold: still running beyond 24h triggers the fallback.
	TimeoutHours = 24
)

// Deps holds all dependencies of the resident monitor jobs (injected by the app assembly line).
//
// Aligned with the accessors exposed by the old creation.Service (CompensationDao/ProjectDao/RiskStatsDao/
// RiskAuditRecordDao); in the new architecture compensation/timeout/risk-governance are carried by each context's application services and infrastructure:
//   - Compensation retry: RiskAuditApp.RetryAsyncAudit + CallbackDispatcher.RetryCallbackAgent
//   - Timeout fallback: Projects.GetStuckProjects + PostCreate.RefundAndFailTimeout
//   - Risk governance: AuditStatsConsumer (dual-cursor consumption) + RiskOfflineJob (threshold evaluation writing the blacklist)
type Deps struct {
	// DB is the data source of the risk-governance poller (dual cursors query cg_risk_audit_record directly).
	DB *gorm.DB
	// PostCreate is the timeout-fallback executor (creation.application.PostCreateApp implements TimeoutBailout).
	PostCreate TimeoutBailout
	// RiskAudit carries async risk-compensation retries (RiskAuditApp.RetryAsyncAudit).
	RiskAudit *riskapp.RiskAuditApp
	// Dispatcher carries callback-agent compensation retries (CallbackDispatcher.RetryCallbackAgent).
	Dispatcher *dao.CallbackDispatcher
	// Projects is the work repository (the timeout job scans stuck projects).
	Projects creationdomain.ProjectRepository
	// Compensations is the compensation repository (the compensation job scans status=0 records).
	Compensations creationdomain.CompensationRepository
	// RiskStats is the risk-hit statistics repository (read by the evaluation job, written by the consumer).
	RiskStats riskdomain.RiskStatsRepository
	// Redis serves risk-governance cursor persistence + blacklist writes (degrading to memory/skip when rdb is nil).
	Redis  *redis.Client
	Logger *zap.Logger
}

// TimeoutBailout is the timeout-fallback executor contract (implemented by creation.application.PostCreateApp).
//
// monitor depends only on this minimal interface for timeout-fallback execution (touching no other creation details);
// PostCreateApp.RefundAndFailTimeout matches the signature and is assigned directly at assembly time.
type TimeoutBailout interface {
	RefundAndFailTimeout(ctx context.Context, project *creationdomain.Project) error
}

// Compile-time assertion: creation.application.PostCreateApp implements TimeoutBailout.
// It could live in a _test package, but here it surfaces assembly errors earlier (the app assembly line assigns it directly).
var _ TimeoutBailout = (*creationapp.PostCreateApp)(nil)

// LoopRun runs the fallback jobs (compensation retry + timeout fallback + risk governance) resident, driven by tickers.
//
// ctx comes from the app's graceful shutdown (goroutines exit after cancel); a single job panic is recovered
// without affecting the next run. rdb serves risk-governance consumption/evaluation.
func LoopRun(ctx context.Context, deps Deps) {
	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	compTicker := time.NewTicker(CompensationJobInterval)
	timeoutTicker := time.NewTicker(TimeoutJobInterval)
	defer compTicker.Stop()
	defer timeoutTicker.Stop()

	// Run each job once at startup (do not wait for the first tick)
	go runLoop(ctx, "compensation", func() { runCompensationSafely(ctx, deps, logger) }, compTicker.C)
	go runLoop(ctx, "timeout", func() { runTimeoutSafely(ctx, deps, logger) }, timeoutTicker.C)

	// Risk governance (stages 6/7): resident consumption of risk_audit_record hits + threshold evaluation for offlining
	if deps.DB != nil && deps.RiskStats != nil {
		poller := riskinfra.NewAuditRecordPoller(deps.DB)
		consumer := riskinfra.NewAuditStatsConsumer(
			poller,
			deps.RiskStats,
			riskinfra.NewRedisCursor(deps.Redis),
			logger,
		)
		riskTicker := time.NewTicker(RiskOfflineJobInterval)
		defer riskTicker.Stop()
		go runLoop(ctx, "risk-stats", func() {
			if err := consumer.RunOnce(ctx); err != nil {
				logger.Error("risk stats consumer failed", zap.Error(err))
			}
		}, riskTicker.C)
		go runLoop(ctx, "risk-offline", func() {
			RunRiskOfflineJobSafely(ctx, deps.RiskStats, deps.Redis, logger)
		}, riskTicker.C)
	}

	<-ctx.Done()
}

// runLoop is the generic timed loop: fn runs on each tick; panics are recovered (a single failure does not affect later runs).
func runLoop(ctx context.Context, name string, fn func(), tick <-chan time.Time) {
	defer func() {
		if r := recover(); r != nil {
			// Top-level fallback: the goroutine never exits on panic (fn already recovers internally; this is double insurance)
			_ = r
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			fn()
		}
	}
}

// runCompensationSafely runs the compensation job wrapped in a recover layer.
func runCompensationSafely(ctx context.Context, deps Deps, logger *zap.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("compensation job panic", zap.Any("panic", r))
		}
	}()
	if err := RunCompensationJob(ctx, deps); err != nil {
		logger.Error("compensation job failed", zap.Error(err))
	}
}

// runTimeoutSafely runs the timeout job wrapped in a recover layer.
func runTimeoutSafely(ctx context.Context, deps Deps, logger *zap.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("timeout job panic", zap.Any("panic", r))
		}
	}()
	if err := RunTimeoutJob(ctx, deps, TimeoutHours); err != nil {
		logger.Error("timeout job failed", zap.Error(err))
	}
}
