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

// 定时任务周期（对齐原项目 monitor 的常驻 job 节奏）。
const (
	// CompensationJobInterval 补偿重试周期：每 1 分钟扫一次。
	CompensationJobInterval = 1 * time.Minute
	// TimeoutJobInterval 超时兜底周期：每 5 分钟扫一次。
	TimeoutJobInterval = 5 * time.Minute
	// TimeoutHours 作品生成超时阈值：超过 24h 仍在 running 则兜底。
	TimeoutHours = 24
)

// Deps 是 monitor 常驻 job 的全部依赖（由 app 装配线注入）。
//
// 对齐旧 creation.Service 暴露的 accessor（CompensationDao/ProjectDao/RiskStatsDao/
// RiskAuditRecordDao），新架构下补偿/超时/风控治理分别由各上下文的应用服务与基础设施承担：
//   - 补偿重试：RiskAuditApp.RetryAsyncAudit + CallbackDispatcher.RetryCallbackAgent
//   - 超时兜底：Projects.GetStuckProjects + PostCreate.RefundAndFailTimeout
//   - 风控治理：AuditStatsConsumer（双游标消费）+ RiskOfflineJob（阈值评估写黑名单）
type Deps struct {
	// DB 是风控治理轮询器的数据源（双游标直查 cg_risk_audit_record）。
	DB *gorm.DB
	// PostCreate 是超时兜底执行器（creation.application.PostCreateApp 实现 TimeoutBailout）。
	PostCreate TimeoutBailout
	// RiskAudit 承担异步风控补偿重试（RiskAuditApp.RetryAsyncAudit）。
	RiskAudit *riskapp.RiskAuditApp
	// Dispatcher 承担回调 agent 补偿重试（CallbackDispatcher.RetryCallbackAgent）。
	Dispatcher *dao.CallbackDispatcher
	// Projects 是作品仓储（超时 job 扫描 stuck 项目）。
	Projects creationdomain.ProjectRepository
	// Compensations 是补偿仓储（补偿 job 扫描 status=0 的记录）。
	Compensations creationdomain.CompensationRepository
	// RiskStats 是风控命中统计仓储（评估 job 读、消费写）。
	RiskStats riskdomain.RiskStatsRepository
	// Redis 供风控治理游标持久化 + 黑名单写入（rdb 为 nil 时退化为内存/跳过）。
	Redis  *redis.Client
	Logger *zap.Logger
}

// TimeoutBailout 超时兜底执行器契约（creation.application.PostCreateApp 实现）。
//
// monitor 只依赖本最小接口承接超时兜底执行（不触碰 creation 其它细节），
// PostCreateApp.RefundAndFailTimeout 签名匹配，装配时直接赋值。
type TimeoutBailout interface {
	RefundAndFailTimeout(ctx context.Context, project *creationdomain.Project) error
}

// 编译期断言：creation.application.PostCreateApp 实现 TimeoutBailout。
// 放在 _test 同包也可以，但这里更早暴露装配错误（app 装配线直接赋值）。
var _ TimeoutBailout = (*creationapp.PostCreateApp)(nil)

// LoopRun 常驻运行兜底 job（补偿重试 + 超时兜底 + 风控治理），由 ticker 驱动。
//
// ctx 来自 app 优雅关机（cancel 后 goroutine 退出）；单个 job panic 会被 recover，
// 不影响下次执行。rdb 供风控治理消费/评估使用。
func LoopRun(ctx context.Context, deps Deps) {
	logger := deps.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	compTicker := time.NewTicker(CompensationJobInterval)
	timeoutTicker := time.NewTicker(TimeoutJobInterval)
	defer compTicker.Stop()
	defer timeoutTicker.Stop()

	// 启动时先各跑一次（不等到第一个 tick）
	go runLoop(ctx, "compensation", func() { runCompensationSafely(ctx, deps, logger) }, compTicker.C)
	go runLoop(ctx, "timeout", func() { runTimeoutSafely(ctx, deps, logger) }, timeoutTicker.C)

	// 风控治理（阶段 6/7）：常驻消费 risk_audit_record 命中 + 阈值评估下线
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

// runLoop 通用的定时循环：每次 tick 执行 fn，panic 被 recover（单次失败不影响后续）。
func runLoop(ctx context.Context, name string, fn func(), tick <-chan time.Time) {
	defer func() {
		if r := recover(); r != nil {
			// 顶层兜底：goroutine 不因 panic 退出（虽然 fn 内部已 recover，这里双保险）
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

// runCompensationSafely 包一层 recover 的补偿 job 执行。
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

// runTimeoutSafely 包一层 recover 的超时 job 执行。
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
