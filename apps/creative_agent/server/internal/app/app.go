package app

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	appkitfake "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/fake"
	appkitllm "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/llm"
	appkitskill "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/skill"
	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/fastpath"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/worker"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/executor"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	creationHandler "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/controller"
	creationinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/dao"
	creationdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	creationapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/database"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/baseapi"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/fakegen"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/monitor"
	pointsapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/application"
	pointsinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/infrastructure"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	riskHandler "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/controller"
	riskinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	riskdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	riskapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/router"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/cache"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/logger"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"

	"github.com/getsentry/sentry-go"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// App contains the runtime dependencies for the API service.
type App struct {
	Config *config.Config
	DB     *gorm.DB
	Redis  *redis.Client
	Logger *zap.Logger
	server *http.Server

	// Phase 6: 模拟生成服务（本地联调用，随机延迟回调自身回调入口）。
	fakeGenServer *fakegen.Server

	// OTel: 可观测性
	TracerProvider trace.TracerProvider

	// NX 锁（心跳/幂等）：Redis 可用时跨进程，否则进程内降级。
	locker infra.Locker

	// Phase 2 async pipeline
	asyncRepo   repository.AsyncRequestRepository
	chunkStore  infra.ChunkStore
	streamQ     infra.StreamQueue
	msgRepo     repository.MessageRecordRepository
	thoughtRepo repository.AgentThoughtRepository
	ideaKing    ideaking.IdeaKingProvider

	// Phase 3 subtask pipeline
	taskRunRepo  repository.TaskRunRepository
	taskStepRepo repository.TaskStepRepository
	taskStreamQ  infra.StreamQueue
	submitter    *subtaskexecutor.Submitter
	taskExecutor *subtaskexecutor.TaskExecutor
	consumer     *taskflow.Consumer
	poller       *taskflow.Poller

	// Phase 4: cron 任务登记 DAO
	cronRepo repository.CronTaskRepository

	// Phase 4: cron 多 agent 自动推进轮询器
	cronPoller *taskflow.CronTaskPoller

	// Phase 4: 快捷路径 + 意图记忆（异步 Worker 接线用）
	fastPath *fastpath.FastPath
	memory   *memory.IntentMemory

	// base-server 域：monitor 常驻 job 的依赖（补偿重试 + 超时兜底 + 风控治理）。
	riskAudit     *riskapp.RiskAuditApp
	dispatcher    *creationinfra.CallbackDispatcher
	postCreate    *creationapp.PostCreateApp            // 超时兜底执行器（RefundAndFailTimeout）
	projects      creationdomain.ProjectRepository      // creation 域（monitor 超时 job 用）
	compensations creationdomain.CompensationRepository // 补偿表（monitor 补偿 job 用）
	riskStats     riskdomain.RiskStatsRepository        // 风控治理统计（评估 job 读、消费写）
	monitorCancel context.CancelFunc                    // 停止常驻 monitor job 的 cancel（优雅关机时调用）
}

// New initializes infrastructure, services, routes, and the HTTP server.
//
// 装配线（依赖倒置：应用层只依赖接口，基础设施由本处装配注入）：
//  1. agent 域：auth/user + conversation + subtask + async worker 依赖
//  2. points 上下文：repo → PointsApp
//  3. risk 上下文：backend + records + 补偿 sink + 黑名单 → RiskAuditApp / AuditOutputApp
//  4. creation 上下文：projects + records + comp + 跨域端口 → CreationApp + CallbackDispatcher
//  5. 组装 handlers → router.Setup
func New(cfg *config.Config) (*App, error) {
	zapLogger := logger.New(cfg.Log.Level)
	zap.ReplaceGlobals(zapLogger)

	// Sentry 错误上报（DSN 为空时不启用；Recovery 中间件自动捕获 panic 上报）。
	if cfg.Sentry.DSN != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: cfg.Sentry.DSN}); err != nil {
			zapLogger.Warn("sentry init failed, error reporting disabled", zap.Error(err))
		} else {
			zapLogger.Info("sentry error reporting enabled")
		}
	}

	// OpenTelemetry 初始化（trace → Jaeger）。
	// 启用时设置全局 provider + W3C propagator；HTTP 中间件读入站 traceparent 建 child span。
	otelProvider, err := otelx.Init(cfg.OTel, zapLogger)
	if err != nil {
		zapLogger.Warn("otel init failed, trace disabled", zap.Error(err))
	}
	zapLogger.Info("otel initialized",
		zap.Bool("enabled", cfg.OTel.Enabled),
		zap.String("endpoint", cfg.OTel.ExporterEndpoint))

	db, err := database.Init(cfg.MySQL, zapLogger)
	if err != nil {
		return nil, err
	}

	// 雪花 ID 生成器（task_run / task_step / message 业务主键）。
	if err := model.InitSnowflake(1); err != nil {
		return nil, err
	}

	rdb, err := cache.NewRedisClient(cfg.Redis)
	if err != nil {
		zapLogger.Warn("redis unavailable, falling back to in-memory infra", zap.Error(err))
		rdb = nil
	}

	// NX 锁：Redis 可用时跨进程；否则进程内实现（单二进制部署仍然可用）。
	var locker infra.Locker = infra.NewMemoryLocker()
	if rdb != nil {
		locker = infra.NewRedisLocker(rdb)
	}

	// 真实 LLM（配置了 base_url 才启用；未配置回退 FakeLLM）。
	// DefaultRealLLM 是子 Agent 通用注入点（auto tool_choice）；
	// DefaultManagerLLM 是管家专用（强制 tool_choice=required）。
	if cfg.LLM.BaseURL != "" {
		llmCfg := appkitllm.Config{
			Provider: cfg.LLM.Provider,
			BaseURL:  cfg.LLM.BaseURL,
			APIKey:   cfg.LLM.APIKey,
			Model:    cfg.LLM.Model,
		}
		appkitllm.DefaultRealLLM = appkitfake.DefaultLLM(llmCfg)
		managerCfg := llmCfg
		managerCfg.ToolChoice = appkitllm.ToolChoiceRequired
		appkitllm.DefaultManagerLLM = appkitfake.DefaultLLM(managerCfg)
		zapLogger.Info("using real LLM",
			zap.String("provider", cfg.LLM.Provider),
			zap.String("model", cfg.LLM.Model))
	} else {
		appkitllm.DefaultRealLLM = nil
		appkitllm.DefaultManagerLLM = nil
		zapLogger.Info("LLM 未配置，使用 FakeLLM（查表模拟）")
	}

	authService := service.NewAuthService(cfg.JWT.Secret)
	userRepo := repository.NewUserRepository(db)
	userSvc := service.NewUserService(userRepo, rdb, authService)

	sessionRepo := repository.NewSessionRepository(db)
	messageRecordRepo := repository.NewMessageRecordRepository(db)
	thoughtRepo := repository.NewAgentThoughtRepository(db)

	// 热点/选题检索 provider：配置了 IDEA_KING_BASE_URL 用自建 HTTP 服务；
	// 否则默认用百度热搜（top.baidu.com 公开接口），网络失败降级 Mock（demo 恒可跑）。
	var ideaKing ideaking.IdeaKingProvider = ideaking.NewFallbackProvider(
		ideaking.NewBaiduHotProvider(), ideaking.NewMockProvider())
	if cfg.IdeaKing.BaseURL != "" {
		ideaKing = ideaking.NewHTTPProvider(ideaking.HTTPConfig{BaseURL: cfg.IdeaKing.BaseURL})
	}

	// Phase 2: Async pipeline infrastructure。
	// Redis 可用时用 Redis 实现（跨进程）；否则降级进程内实现（yukino_cache + 内存队列）。
	var asyncRepo repository.AsyncRequestRepository
	var chunkStore infra.ChunkStore
	var streamQ infra.StreamQueue

	if cfg.Async.WorkerID == "" {
		cfg.Async.WorkerID = "worker-1"
	}

	if cfg.Async.Enabled {
		asyncRepo = repository.NewAsyncRequestRepository(db)
		if rdb != nil {
			chunkStore = infra.NewRedisChunkStore(rdb)
			streamQ = infra.NewRedisStreamQueue(rdb, "cg:stream:conversation", "cg:workers", cfg.Async.WorkerID)
		} else {
			chunkStore = infra.NewMemoryChunkStore()
			streamQ = infra.NewMemoryStreamQueue("cg:stream:conversation", "cg:workers", cfg.Async.WorkerID)
		}
	}

	// Phase 3: Subtask pipeline infrastructure
	var taskRunRepo repository.TaskRunRepository
	var taskStepRepo repository.TaskStepRepository
	var taskStreamQ infra.StreamQueue
	var submitter *subtaskexecutor.Submitter
	var taskExecutor *subtaskexecutor.TaskExecutor
	var taskConsumer *taskflow.Consumer
	var taskPoller *taskflow.Poller
	var cronRepo repository.CronTaskRepository
	var cronPoller *taskflow.CronTaskPoller

	// Phase 5: SKILL.md 动态工具源。扫描仓库根 skills/ 目录。
	skillRepo, err := appkitskill.NewFSRepository("skills")
	if err != nil {
		zapLogger.Warn("skills 目录加载失败，skill 工具不可用", zap.Error(err))
	}
	var skillProvider *appkitskill.SkillProvider
	if skillRepo != nil {
		skillProvider = appkitskill.NewSkillProvider(skillRepo, appkitskill.NewRemoteSkillExecutor(nil))
	}

	// Phase 6: base-server 域已并入本进程：HTTP client 默认指向自身 loopback，
	// 工具经 /inner/* 调用同进程的 creation/risk 链路；fakegen 模拟生成服务照旧。
	baseURL := cfg.BaseServer.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:" + cfg.Server.Port
	}
	baseClient := baseapi.NewClient(baseapi.ClientConfig{BaseURL: baseURL})
	zapLogger.Info("base-server client configured", zap.String("baseURL", baseURL))

	var fakeGenServer *fakegen.Server
	if cfg.FakeGen.Enabled {
		// fakegen listen 失败（如端口占用）会 panic，降级为不可用 + 回退假实现，不炸掉整个启动。
		func() {
			defer func() {
				if r := recover(); r != nil {
					zapLogger.Warn("fakegen server unavailable, falling back to fake impl",
						zap.Any("recover", r))
					fakeGenServer = nil
				}
			}()
			fakeGenServer = fakegen.NewServer(fakegen.Config{
				Addr:   ":" + cfg.FakeGen.Port,
				Logger: zapLogger,
			})
			zapLogger.Info("fakegen server configured", zap.String("addr", ":"+cfg.FakeGen.Port))
		}()
	}

	// Phase 6: 工具注册——注入 base-server client + fakegen + 回调地址（未配置回退假实现）。
	// 回调入口在自身 /aigc/callback/task_detail（fakegen 生成完成后回调推进 Waiting 任务）。
	var tools appkittools.ToolsProvider
	if fakeGenServer != nil {
		tools = appkittools.NewSubAgentToolRegistry(&appkittools.RealRunnerDeps{
			Base:            baseClient,
			FakeGen:         fakeGenServer,
			CallbackBaseURL: "http://localhost:" + cfg.Server.Port,
		})
	} else {
		tools = appkittools.NewSubAgentToolRegistry()
	}

	if cfg.Async.Enabled {
		taskRunRepo = repository.NewTaskRunRepository(db)
		taskStepRepo = repository.NewTaskStepRepository(db)
		if rdb != nil {
			taskStreamQ = infra.NewRedisStreamQueue(rdb, "cg:stream:task", "cg:executors", cfg.Async.WorkerID)
		} else {
			taskStreamQ = infra.NewMemoryStreamQueue("cg:stream:task", "cg:executors", cfg.Async.WorkerID)
		}

		// Phase 4: cron 登记 DAO（Worker 的 requestType=cron 注册用）。
		cronRepo = repository.NewCronTaskRepository(db)

		if skillProvider != nil {
			tools = appkittools.NewCompositeProvider(tools, skillProvider)
		}
		taskExecutor = subtaskexecutor.NewTaskExecutor(subtaskexecutor.TaskExecutorConfig{
			TaskRunRepo: taskRunRepo,
			StepRepo:    taskStepRepo,
			Tools:       tools,
			Logger:      zapLogger,
		})
		taskConsumer = taskflow.NewConsumer(taskflow.ConsumerConfig{
			StreamQ:     taskStreamQ,
			TaskRunRepo: taskRunRepo,
			Executor:    taskExecutor,
			ExecutorID:  cfg.Async.WorkerID + "-task",
			Logger:      zapLogger,
		})
		taskPoller = taskflow.NewPoller(taskflow.PollerConfig{
			StepRepo:    taskStepRepo,
			TaskRunRepo: taskRunRepo,
			Logger:      zapLogger,
		})
		submitter = subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
			TaskRunRepo: taskRunRepo,
			StreamQ:     taskStreamQ,
			Logger:      zapLogger,
		})

		// Phase 4: Cron 多 agent 自动推进 Poller（默认每 30s 扫 Running cron 任务推进下一步）。
		cronPoller = taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
			CronRepo:    cronRepo,
			TaskRunRepo: taskRunRepo,
			Submitter:   submitter,
			Logger:      zapLogger,
		})
	}

	// Step 3.6: 多 agent 串联的意图记忆（内存版，单进程）。
	intentMemory := memory.NewIntentMemory()

	// Phase 4: 快捷路径。仅在 Async 启用（Submitter 非 nil）时注入，
	// 否则传 nil → SupervisorAgentNode 跳过 FastPath 判断，走正常 ReAct。
	var fastPath *fastpath.FastPath
	if submitter != nil {
		fastPath = fastpath.NewFastPath(fastpath.FastPathConfig{
			Memory:      intentMemory,
			Submitter:   submitter,
			TaskRunRepo: taskRunRepo,
			StepRepo:    taskStepRepo,
			Logger:      zapLogger,
		})
	}

	// ===== base-server 域装配（points / risk / creation）=====

	// points 上下文
	pointsRepo := pointsinfra.NewPointsRepositoryImpl(db)
	pointsApp := pointsapp.NewPointsApp(pointsRepo)

	// risk 基础设施（补偿 sink 由 creation 实现注入，依赖反转）
	riskRecords := riskinfra.NewAuditRecordRepositoryImpl(db)
	riskStatsRepo := riskinfra.NewRiskStatsRepositoryImpl(db)
	auditBackend := riskinfra.NewLocalAuditBackend()

	// creation 基础设施
	projRepo := creationinfra.NewProjectRepositoryImpl(db)
	cbRepo := creationinfra.NewCallbackRecordRepositoryImpl(db)
	compRepo := creationinfra.NewCompensationRepositoryImpl(db)

	// creation → risk 的依赖反转：AuditResultSink（风控回调落定 project）由 creation 实现
	onAuditResult := creationinfra.NewOnAuditResultImpl(projRepo, pointsApp)
	// risk → creation 的依赖反转：CompensationSink（异步风控提交失败写补偿表）由 creation 实现
	compSink := creationinfra.NewCompensationSinkImpl(compRepo)

	blacklistChecker := riskinfra.NewRedisBlacklistChecker(rdb)
	riskAuditApp := riskapp.NewRiskAuditApp(auditBackend, riskRecords, compSink, blacklistChecker)
	auditOutputApp := riskapp.NewAuditOutputApp(riskRecords, onAuditResult)

	// creation 应用门面 + 回调分发器
	creationApp := creationapp.NewCreationApp(projRepo, cbRepo, riskAuditApp, pointsApp)
	dispatcher := creationinfra.NewCallbackDispatcher(
		creationinfra.NewCallbackDeps(projRepo, cbRepo, compRepo, riskAuditApp, pointsApp),
	)

	// base-server 域 handlers
	creationCtl := creationHandler.NewCreationHandler(
		creationApp.PreCreate, creationApp.Create, creationApp.PostCreate, creationApp.HandleError, dispatcher)
	riskCtl := riskHandler.NewRiskHandler(auditOutputApp)

	engine := router.Setup(router.Dependencies{
		UserSvc:           userSvc,
		JWTSecret:         cfg.JWT.Secret,
		Redis:             rdb,
		Logger:            zapLogger,
		CreationHandler:   creationCtl,
		RiskHandler:       riskCtl,
		SessionRepo:       sessionRepo,
		MessageRecordRepo: messageRecordRepo,
		AsyncRepo:         asyncRepo,
		ChunkStore:        chunkStore,
		StreamQ:           streamQ,
		Locker:            locker,
		AsyncEnabled:      cfg.Async.Enabled,
		IntentMemory:      intentMemory,
		FastPath:          fastPath,
		TaskPoller:        taskPoller,
		TaskRunRepo:       taskRunRepo,
		TaskStepRepo:      taskStepRepo,
		TracerProvider:    otelProvider,
		ServiceName:       cfg.OTel.ServiceName,
		MetricsEnabled:    cfg.Metrics.Enabled,
	})

	server := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: engine,
	}

	return &App{
		Config:         cfg,
		DB:             db,
		Redis:          rdb,
		Logger:         zapLogger,
		server:         server,
		fakeGenServer:  fakeGenServer,
		TracerProvider: otelProvider,
		locker:         locker,
		asyncRepo:      asyncRepo,
		chunkStore:     chunkStore,
		streamQ:        streamQ,
		msgRepo:        messageRecordRepo,
		thoughtRepo:    thoughtRepo,
		ideaKing:       ideaKing,
		taskRunRepo:    taskRunRepo,
		taskStepRepo:   taskStepRepo,
		taskStreamQ:    taskStreamQ,
		submitter:      submitter,
		taskExecutor:   taskExecutor,
		consumer:       taskConsumer,
		poller:         taskPoller,
		cronRepo:       cronRepo,
		cronPoller:     cronPoller,
		fastPath:       fastPath,
		memory:         intentMemory,
		riskAudit:      riskAuditApp,
		dispatcher:     dispatcher,
		postCreate:     creationApp.PostCreate,
		projects:       projRepo,
		compensations:  compRepo,
		riskStats:      riskStatsRepo,
	}, nil
}

// Run starts the HTTP server with graceful shutdown.
// Also starts the async Worker and RecoveryScanner if async is enabled.
func (a *App) Run() error {
	a.Logger.Info("server listening", zap.String("addr", a.server.Addr))

	// 启动常驻 monitor jobs（补偿重试 + 超时兜底 + 风控治理），优雅关机时 cancel 停止。
	a.startMonitorJobs()

	// Phase 2: Start async worker goroutines
	var workerCtx context.Context
	var workerCancel context.CancelFunc
	if a.Config.Async.Enabled && a.streamQ != nil {
		workerCtx, workerCancel = context.WithCancel(context.Background())
		defer workerCancel()

		consumer := worker.NewConsumer(worker.ConsumerConfig{
			StreamQ:       a.streamQ,
			ChunkStore:    a.chunkStore,
			Lock:          a.locker,
			AsyncRepo:     a.asyncRepo,
			MsgRepo:       a.msgRepo,
			WorkerID:      a.Config.Async.WorkerID,
			Logger:        a.Logger,
			TaskSubmitter: a.submitter,
			TaskPoller:    a.poller,
			CronRepo:      a.cronRepo,
			TaskRunRepo:   a.taskRunRepo,
			// 业务决策（快路径 / 管家策略+prompt / HIL 触发）由 app 装配注入。
			// worker 本身只做"消费队列 + 锁/CAS/心跳/落库/ACK"。
			Hooks: &worker.TaskRunnerHooks{
				FastPath:           a.fastPath,
				Memory:             a.memory,
				UseManagerStrategy: appkitllm.DefaultManagerLLM != nil,
				CheckpointSave: func(sessionID string, cp *react.Checkpoint) {
					a.memory.SaveCheckpoint(sessionID, cp)
				},
				// HIL 触发词：query 含"确认"（对齐原 consumer 的 strings.Contains 判据）。
				ShouldEnableHIL: func(query string) bool {
					return strings.Contains(query, "确认")
				},
				// 每轮 LLM 落 thought 表（LLM 流式输出恢复副本的中间消息表）。
				ThoughtRepo: a.thoughtRepo,
				// 热点/选题检索 provider（热点卡 strategy 直出用；未配置回退 MockProvider）。
				IdeaKing: a.ideaKing,
			},
		})
		go consumer.Start(workerCtx)

		recovery := worker.NewRecoveryScanner(worker.RecoveryConfig{
			AsyncRepo:  a.asyncRepo,
			MsgRepo:    a.msgRepo,
			ChunkStore: a.chunkStore,
			StreamQ:    a.streamQ,
			Logger:     a.Logger,
		})
		go recovery.Start(workerCtx)

		a.Logger.Info("async worker and recovery scanner started",
			zap.String("workerID", a.Config.Async.WorkerID))

		// Phase 3: Start subtask consumer goroutine
		if a.consumer != nil {
			go a.consumer.Start(workerCtx)
			a.Logger.Info("task consumer started",
				zap.String("workerID", a.Config.Async.WorkerID))
		}

		// Phase 4: Cron 多 agent 自动推进 Poller（默认每 30s）。
		if a.cronPoller != nil {
			interval := a.Config.Cron.PollInterval.Std()
			if interval <= 0 {
				interval = 30 * time.Second
			}
			go a.cronPoller.Start(workerCtx, interval)
			a.Logger.Info("cron task poller started",
				zap.Duration("interval", interval),
				zap.String("workerID", a.Config.Async.WorkerID))
		}
	}

	// Phase 6: 启动模拟生成服务（本地联调用，收到 /submit 后随机延迟回调自身回调入口）。
	// 不依赖 Redis/workerCtx，故用独立 context.Background()；生命周期由优雅关机段的 Shutdown 收口。
	if a.fakeGenServer != nil {
		go func() {
			if err := a.fakeGenServer.Start(context.Background()); err != nil {
				a.Logger.Warn("fakegen server stopped", zap.Error(err))
			}
		}()
		a.Logger.Info("fakegen server started")
	}

	go func() {
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Logger.Fatal("server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	a.Logger.Info("shutting down server", zap.String("signal", sig.String()))

	// 停止 monitor jobs（job 内部 select ctx.Done，立即退出）
	if a.monitorCancel != nil {
		a.monitorCancel()
	}

	// Cancel worker context before DB close
	if workerCancel != nil {
		workerCancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.server.Shutdown(ctx); err != nil {
		a.Logger.Error("server forced to shutdown", zap.Error(err))
		return err
	}

	// Phase 6: 关闭模拟生成服务（与主 HTTP server 同一超时 ctx）。
	if a.fakeGenServer != nil {
		if err := a.fakeGenServer.Shutdown(ctx); err != nil {
			a.Logger.Warn("fakegen shutdown error", zap.Error(err))
		}
	}

	// Flush trace 数据到 Jaeger。
	if a.TracerProvider != nil {
		if tp, ok := a.TracerProvider.(interface {
			Shutdown(context.Context) error
		}); ok {
			if err := tp.Shutdown(ctx); err != nil {
				a.Logger.Warn("otel tracer shutdown error", zap.Error(err))
			}
		}
	}

	// Flush Sentry 事件缓冲。
	sentry.Flush(2 * time.Second)

	a.Logger.Info("server exited gracefully")
	return nil
}

// startMonitorJobs 启动补偿重试 + 超时兜底 + 风控治理常驻定时任务。
// Redis 不可用（rdb==nil）时 job 仍启动——补偿/超时只依赖 DB，不依赖 Redis。
//
// ⚠️ 异步启动：monitor.LoopRun 内部 `<-ctx.Done()` 阻塞直到优雅关机，若同步调用
// 会卡住主 goroutine，导致后面的 ListenAndServe 永不执行（服务不监听端口）。
// 必须在 goroutine 中运行。
func (a *App) startMonitorJobs() {
	if a.monitorCancel != nil {
		return // 已启动，避免重复
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.monitorCancel = cancel
	go monitor.LoopRun(ctx, monitor.Deps{
		DB:            a.DB,
		PostCreate:    a.postCreate,
		RiskAudit:     a.riskAudit,
		Dispatcher:    a.dispatcher,
		Projects:      a.projects,
		Compensations: a.compensations,
		RiskStats:     a.riskStats,
		Redis:         a.Redis,
		Logger:        a.Logger,
	})
}

// TestHandler returns the underlying HTTP handler for testing purposes.
func (a *App) TestHandler() http.Handler {
	return a.server.Handler
}
