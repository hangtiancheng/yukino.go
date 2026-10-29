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

	// Phase 6: simulated generation service (for local integration; calls back its own callback entry after a random delay).
	fakeGenServer *fakegen.Server

	// OTel: observability
	TracerProvider trace.TracerProvider

	// NX lock (heartbeat/idempotency): cross-process when Redis is available, otherwise in-process degradation.
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

	// Phase 4: cron-task registration DAO
	cronRepo repository.CronTaskRepository

	// Phase 4: cron multi-agent auto-advance poller
	cronPoller *taskflow.CronTaskPoller

	// Phase 4: fast path + intent memory (for async Worker wiring)
	fastPath *fastpath.FastPath
	memory   *memory.IntentMemory

	// base-server domain: dependencies of the resident monitor jobs (compensation retry + timeout fallback + risk governance).
	riskAudit     *riskapp.RiskAuditApp
	dispatcher    *creationinfra.CallbackDispatcher
	postCreate    *creationapp.PostCreateApp            // timeout-fallback executor (RefundAndFailTimeout)
	projects      creationdomain.ProjectRepository      // creation domain (used by the monitor timeout job)
	compensations creationdomain.CompensationRepository // compensation table (used by the monitor compensation job)
	riskStats     riskdomain.RiskStatsRepository        // risk-governance statistics (read by the evaluation job, written by the consumer)
	monitorCancel context.CancelFunc                    // cancel that stops the resident monitor jobs (called on graceful shutdown)
}

// New initializes infrastructure, services, routes, and the HTTP server.
//
// Assembly line (dependency inversion: application layers depend on interfaces only; infrastructure is wired and injected here):
//  1. agent domain: auth/user + conversation + subtask + async worker dependencies
//  2. points context: repo → PointsApp
//  3. risk context: backend + records + compensation sink + blacklist → RiskAuditApp / AuditOutputApp
//  4. creation context: projects + records + comp + cross-domain ports → CreationApp + CallbackDispatcher
//  5. assemble handlers → router.Setup
func New(cfg *config.Config) (*App, error) {
	zapLogger := logger.New(cfg.Log.Level)
	zap.ReplaceGlobals(zapLogger)

	// Sentry error reporting (disabled when the DSN is empty; the Recovery middleware automatically captures and reports panics).
	if cfg.Sentry.DSN != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: cfg.Sentry.DSN}); err != nil {
			zapLogger.Warn("sentry init failed, error reporting disabled", zap.Error(err))
		} else {
			zapLogger.Info("sentry error reporting enabled")
		}
	}

	// OpenTelemetry initialization (trace → Jaeger).
	// When enabled it sets the global provider + W3C propagator; the HTTP middleware reads the inbound traceparent and creates a child span.
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

	// Snowflake ID generator (business primary keys for task_run / task_step / message).
	if err := model.InitSnowflake(1); err != nil {
		return nil, err
	}

	rdb, err := cache.NewRedisClient(cfg.Redis)
	if err != nil {
		zapLogger.Warn("redis unavailable, falling back to in-memory infra", zap.Error(err))
		rdb = nil
	}

	// NX lock: cross-process when Redis is available; otherwise the in-process implementation (still usable for single-binary deployments).
	var locker infra.Locker = infra.NewMemoryLocker()
	if rdb != nil {
		locker = infra.NewRedisLocker(rdb)
	}

	// Real LLM (enabled only when base_url is configured; falls back to FakeLLM otherwise).
	// DefaultRealLLM is the generic sub-agent injection point (auto tool_choice);
	// DefaultManagerLLM is manager-specific (forced tool_choice=required).
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
		zapLogger.Info("LLM not configured, using FakeLLM (table-lookup simulation)")
	}

	authService := service.NewAuthService(cfg.JWT.Secret)
	userRepo := repository.NewUserRepository(db)
	userSvc := service.NewUserService(userRepo, rdb, authService)

	sessionRepo := repository.NewSessionRepository(db)
	messageRecordRepo := repository.NewMessageRecordRepository(db)
	thoughtRepo := repository.NewAgentThoughtRepository(db)

	// Hotspot/topic retrieval provider: with IDEA_KING_BASE_URL configured it uses the self-hosted HTTP service;
	// otherwise it defaults to Baidu hot search (top.baidu.com public API), degrading to Mock on network failure (the demo always runs).
	var ideaKing ideaking.IdeaKingProvider = ideaking.NewFallbackProvider(
		ideaking.NewBaiduHotProvider(), ideaking.NewMockProvider())
	if cfg.IdeaKing.BaseURL != "" {
		ideaKing = ideaking.NewHTTPProvider(ideaking.HTTPConfig{BaseURL: cfg.IdeaKing.BaseURL})
	}

	// Phase 2: Async pipeline infrastructure.
	// Redis implementations when Redis is available (cross-process); otherwise in-process degradation (yukino_cache + in-memory queue).
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

	// Phase 5: SKILL.md dynamic tool source. Scans the repository-root skills/ directory.
	skillRepo, err := appkitskill.NewFSRepository("skills")
	if err != nil {
		zapLogger.Warn("failed to load the skills directory, skill tools unavailable", zap.Error(err))
	}
	var skillProvider *appkitskill.SkillProvider
	if skillRepo != nil {
		skillProvider = appkitskill.NewSkillProvider(skillRepo, appkitskill.NewRemoteSkillExecutor(nil))
	}

	// Phase 6: the base-server domain is merged into this process: the HTTP client defaults to its own loopback;
	// tools call the same-process creation/risk chain via /inner/*; the fakegen simulated generation service stays as before.
	baseURL := cfg.BaseServer.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:" + cfg.Server.Port
	}
	baseClient := baseapi.NewClient(baseapi.ClientConfig{BaseURL: baseURL})
	zapLogger.Info("base-server client configured", zap.String("baseURL", baseURL))

	var fakeGenServer *fakegen.Server
	if cfg.FakeGen.Enabled {
		// A fakegen listen failure (e.g. port occupied) panics; degrade to unavailable + fall back to the fake implementation instead of blowing up startup.
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

	// Phase 6: tool registration — inject the base-server client + fakegen + callback address (falling back to fake implementations when unconfigured).
	// The callback entry is its own /aigc/callback/task_detail (fakegen calls back after generation completes, advancing Waiting tasks).
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

		// Phase 4: cron registration DAO (for the Worker's requestType=cron registration).
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

		// Phase 4: Cron multi-agent auto-advance Poller (by default scans Running cron tasks every 30s and advances the next step).
		cronPoller = taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
			CronRepo:    cronRepo,
			TaskRunRepo: taskRunRepo,
			Submitter:   submitter,
			Logger:      zapLogger,
		})
	}

	// Step 3.6: intent memory for multi-agent chaining (in-memory, single process).
	intentMemory := memory.NewIntentMemory()

	// Phase 4: fast path. Injected only when Async is enabled (Submitter non-nil);
	// otherwise nil is passed → SupervisorAgentNode skips the FastPath check and runs normal ReAct.
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

	// ===== base-server domain assembly (points / risk / creation) =====

	// points context
	pointsRepo := pointsinfra.NewPointsRepositoryImpl(db)
	pointsApp := pointsapp.NewPointsApp(pointsRepo)

	// risk infrastructure (the compensation sink is injected by the creation implementation; dependency inversion)
	riskRecords := riskinfra.NewAuditRecordRepositoryImpl(db)
	riskStatsRepo := riskinfra.NewRiskStatsRepositoryImpl(db)
	auditBackend := riskinfra.NewLocalAuditBackend()

	// creation infrastructure
	projRepo := creationinfra.NewProjectRepositoryImpl(db)
	cbRepo := creationinfra.NewCallbackRecordRepositoryImpl(db)
	compRepo := creationinfra.NewCompensationRepositoryImpl(db)

	// creation → risk dependency inversion: AuditResultSink (the risk callback settling the project) is implemented by creation
	onAuditResult := creationinfra.NewOnAuditResultImpl(projRepo, pointsApp)
	// risk → creation dependency inversion: CompensationSink (writing the compensation table when async risk submission fails) is implemented by creation
	compSink := creationinfra.NewCompensationSinkImpl(compRepo)

	blacklistChecker := riskinfra.NewRedisBlacklistChecker(rdb)
	riskAuditApp := riskapp.NewRiskAuditApp(auditBackend, riskRecords, compSink, blacklistChecker)
	auditOutputApp := riskapp.NewAuditOutputApp(riskRecords, onAuditResult)

	// creation application facade + callback dispatcher
	creationApp := creationapp.NewCreationApp(projRepo, cbRepo, riskAuditApp, pointsApp)
	dispatcher := creationinfra.NewCallbackDispatcher(
		creationinfra.NewCallbackDeps(projRepo, cbRepo, compRepo, riskAuditApp, pointsApp),
	)

	// base-server domain handlers
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

	// Start the resident monitor jobs (compensation retry + timeout fallback + risk governance); cancel stops them on graceful shutdown.
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
			// Business decisions (fast path / manager strategy+prompt / HIL triggering) are injected by the app wiring.
			// The worker itself only does "queue consumption + lock/CAS/heartbeat/persistence/ACK".
			Hooks: &worker.TaskRunnerHooks{
				FastPath:           a.fastPath,
				Memory:             a.memory,
				UseManagerStrategy: appkitllm.DefaultManagerLLM != nil,
				CheckpointSave: func(sessionID string, cp *react.Checkpoint) {
					a.memory.SaveCheckpoint(sessionID, cp)
				},
				// HIL trigger word: the query contains "确认" (aligned with the original consumer's strings.Contains criterion).
				ShouldEnableHIL: func(query string) bool {
					return strings.Contains(query, "确认")
				},
				// Persist a thought row per LLM round (the intermediate message table for LLM streaming-output recovery copies).
				ThoughtRepo: a.thoughtRepo,
				// Hotspot/topic retrieval provider (for the hot-card strategy's direct emission; falls back to MockProvider when unconfigured).
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

		// Phase 4: Cron multi-agent auto-advance Poller (every 30s by default).
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

	// Phase 6: start the simulated generation service (for local integration; after receiving /submit it calls back its own callback entry after a random delay).
	// It does not depend on Redis/workerCtx, so it uses an independent context.Background(); its lifecycle is closed by Shutdown in the graceful-shutdown section.
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

	// Stop the monitor jobs (jobs select on ctx.Done internally and exit immediately)
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

	// Phase 6: shut down the simulated generation service (with the same timeout ctx as the main HTTP server).
	if a.fakeGenServer != nil {
		if err := a.fakeGenServer.Shutdown(ctx); err != nil {
			a.Logger.Warn("fakegen shutdown error", zap.Error(err))
		}
	}

	// Flush trace data to Jaeger.
	if a.TracerProvider != nil {
		if tp, ok := a.TracerProvider.(interface {
			Shutdown(context.Context) error
		}); ok {
			if err := tp.Shutdown(ctx); err != nil {
				a.Logger.Warn("otel tracer shutdown error", zap.Error(err))
			}
		}
	}

	// Flush the Sentry event buffer.
	sentry.Flush(2 * time.Second)

	a.Logger.Info("server exited gracefully")
	return nil
}

// startMonitorJobs starts the resident scheduled jobs for compensation retry + timeout fallback + risk governance.
// The jobs start even when Redis is unavailable (rdb==nil) — compensation/timeout depend only on the DB, not Redis.
//
// ⚠️ Started asynchronously: monitor.LoopRun blocks on `<-ctx.Done()` until graceful shutdown; calling it synchronously
// would stall the main goroutine so the subsequent ListenAndServe never runs (the service never listens).
// It must run in a goroutine.
func (a *App) startMonitorJobs() {
	if a.monitorCancel != nil {
		return // already started; avoid duplicates
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
