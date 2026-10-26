package router

import (
	"bytes"
	"net/http"

	conversation "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/fastpath"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	creationHandler "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/controller"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/handler"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/middleware"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	riskHandler "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/controller"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Dependencies contains the services required by the HTTP router.
type Dependencies struct {
	UserSvc   *service.UserService
	JWTSecret string
	Redis     *redis.Client
	Logger    *zap.Logger

	// base-server 域 handlers（creation / risk；nil 时跳过对应路由）。
	CreationHandler *creationHandler.CreationHandler
	RiskHandler     *riskHandler.RiskHandler

	// Conversation repositories.
	SessionRepo       repository.SessionRepository
	MessageRecordRepo repository.MessageRecordRepository

	// Phase 2 async pipeline.
	AsyncRepo    repository.AsyncRequestRepository
	ChunkStore   infra.ChunkStore
	StreamQ      infra.StreamQueue
	Locker       infra.Locker
	AsyncEnabled bool

	// Phase 4: 快捷路径（nil 时跳过 FastPath 判断，走 ReAct）。
	FastPath *fastpath.FastPath

	// Phase 4: 同步快任务回流（Async 启用时非 nil）。
	TaskPoller  *taskflow.Poller
	TaskRunRepo repository.TaskRunRepository

	// Task 8: 回调路由依赖（服务端调用，身份来自回调 body）。
	TaskStepRepo repository.TaskStepRepository

	// Step 3.6: 多 agent 串联的意图记忆（nil 时 Setup 自动创建）。
	IntentMemory *memory.IntentMemory

	// OTel: 可观测性。TracerProvider 非 nil 时启用 HTTP span。
	TracerProvider trace.TracerProvider
	// ServiceName 是 OTel 服务名（Jaeger 里按此过滤）。
	ServiceName string

	// MetricsEnabled 控制 /metrics（Prometheus）端点。
	MetricsEnabled bool
}

// Setup 注册所有路由与中间件，返回配置好的 yukino_http Application。
//
// 中间件顺序：RequestID → OTel → Logger → CORS → Recovery。
// 注意：yukino_http 的 Router 中间件按路径前缀生效（同前缀子分组无法只保护部分路由），
// 因此 JWT 用 withAuth 按路由包裹，而不是挂在 /api/v1 子分组上。
func Setup(deps Dependencies) *yukino.Application {
	app := yukino.New()

	app.Use(middleware.RequestID())
	if deps.TracerProvider != nil {
		app.Use(middleware.OTelTracing(deps.ServiceName, deps.TracerProvider))
	}
	app.Use(
		middleware.Logger(deps.Logger),
		middleware.CORS(),
		middleware.Recovery(deps.Logger),
	)

	jwtMW := middleware.JWT(deps.JWTSecret, deps.Redis)
	withAuth := func(h yukino.Middleware) yukino.Middleware {
		return func(ctx *yukino.Context, next func()) {
			jwtMW(ctx, func() { h(ctx, next) })
		}
	}

	authHandler := handler.NewAuthHandler(deps.UserSvc)
	conversationHandler := handler.NewConversationHandler(conversation.FlowDeps{
		SessionRepo:       deps.SessionRepo,
		MessageRecordRepo: deps.MessageRecordRepo,
		AsyncRepo:         deps.AsyncRepo,
		ChunkStore:        deps.ChunkStore,
		StreamQ:           deps.StreamQ,
		Locker:            deps.Locker,
		AsyncEnabled:      deps.AsyncEnabled,
		FastPath:          deps.FastPath,
		TaskPoller:        deps.TaskPoller,
		TaskRunRepo:       deps.TaskRunRepo,
	})

	// Step 3.6: 多 agent 串联的意图记忆（内存版，单进程）。
	if deps.IntentMemory == nil {
		deps.IntentMemory = memory.NewIntentMemory()
	}

	v1 := app.Router("/api/v1")
	v1.Get("/ping", handler.Ping)
	v1.Post("/register", authHandler.Register)
	v1.Post("/login", authHandler.Login)
	v1.Get("/example", withAuth(handler.Example))
	v1.Post("/logout", withAuth(authHandler.Logout))

	// Conversation (Phase 1): /aigc/conversation
	aigc := app.Router("/aigc")
	// Task 8: 回调路由，服务端调用（fakegen/base-server 域），不挂 JWT（身份来自回调 body）。
	aigc.Post("/callback/task_detail",
		handler.CallbackTaskDetailHandler(handler.CallbackDeps{
			StepRepo:    deps.TaskStepRepo,
			TaskRunRepo: deps.TaskRunRepo,
		}))

	// 对话路由强制鉴权（会话绑定个人）。
	aigc.Post("/conversation", withAuth(conversationHandler))
	// GET 版本供浏览器 EventSource 使用（EventSource 只支持 GET）。
	aigc.Get("/conversation", withAuth(conversationHandler))

	// Step 3.6: 点卡确认接口（多 agent 串联的推进器）。
	// 用户点交互卡 → 写回 intent_memory → 推进 FlowStage。
	aigc.Post("/agent/submit_clarification", withAuth(
		handler.SubmitClarificationHandler(handler.ClarificationDeps{
			Memory: deps.IntentMemory,
		})))

	// 内部接口：被 agent 工具链按序调用（同进程 loopback），无需用户 JWT。
	if deps.CreationHandler != nil {
		inner := app.Router("/inner")
		creation := inner.Router("/creation")
		creation.Post("/pre_create", deps.CreationHandler.PreCreate)
		creation.Post("/create", deps.CreationHandler.Create)
		creation.Post("/sync_post_create", deps.CreationHandler.SyncPostCreate)
		creation.Post("/async_post_create", deps.CreationHandler.AsyncPostCreate)
		creation.Post("/handle_error", deps.CreationHandler.HandleError)

		callback := inner.Router("/callback")
		callback.Post("/task_detail", deps.CreationHandler.CallbackTaskDetail)
		// risk 回调由 risk 域独立负责，单独判空守卫（与 creation 装配可解耦）。
		if deps.RiskHandler != nil {
			callback.Post("/risk", deps.RiskHandler.CallbackRisk)
		}
	}

	// Prometheus 指标端点（文本格式，经 prometheus/common expfmt 编码）。
	if deps.MetricsEnabled {
		app.Get("/metrics", MetricsHandler())
	}

	return app
}

// MetricsHandler 以 Prometheus 文本格式暴露默认注册表的指标。
func MetricsHandler() yukino.Middleware {
	return func(ctx *yukino.Context, _ func()) {
		mfs, err := prometheus.DefaultGatherer.Gather()
		if err != nil {
			response.Fail(ctx, http.StatusInternalServerError, response.CodeInternal, "gather metrics failed")
			return
		}
		var buf bytes.Buffer
		enc := expfmt.NewEncoder(&buf, expfmt.NewFormat(expfmt.TypeTextPlain))
		for _, mf := range mfs {
			if err := enc.Encode(mf); err != nil {
				response.Fail(ctx, http.StatusInternalServerError, response.CodeInternal, "encode metrics failed")
				return
			}
		}
		ctx.Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		ctx.SetStatus(http.StatusOK)
		ctx.Data(buf.Bytes())
	}
}
