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

	// base-server domain handlers (creation / risk; the corresponding routes are skipped when nil).
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

	// Phase 4: fast path (when nil, the FastPath check is skipped and ReAct runs).
	FastPath *fastpath.FastPath

	// Phase 4: sync fast-task backflow (non-nil when Async is enabled).
	TaskPoller  *taskflow.Poller
	TaskRunRepo repository.TaskRunRepository

	// Task 8: callback route dependencies (server-to-server calls; identity comes from the callback body).
	TaskStepRepo repository.TaskStepRepository

	// Step 3.6: intent memory for multi-agent chaining (Setup creates it automatically when nil).
	IntentMemory *memory.IntentMemory

	// OTel: observability. HTTP spans are enabled when TracerProvider is non-nil.
	TracerProvider trace.TracerProvider
	// ServiceName is the OTel service name (used for filtering in Jaeger).
	ServiceName string

	// MetricsEnabled controls the /metrics (Prometheus) endpoint.
	MetricsEnabled bool
}

// Setup registers all routes and middleware and returns the configured yukino_http Application.
//
// Middleware order: RequestID → OTel → Logger → CORS → Recovery.
// Note: yukino_http Router middleware applies by path prefix (a sub-group with the same prefix cannot protect only some routes),
// so JWT is wrapped per route with withAuth instead of being mounted on an /api/v1 sub-group.
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

	// Step 3.6: intent memory for multi-agent chaining (in-memory, single process).
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
	// Task 8: callback route, server-to-server (fakegen/base-server domain), no JWT (identity comes from the callback body).
	aigc.Post("/callback/task_detail",
		handler.CallbackTaskDetailHandler(handler.CallbackDeps{
			StepRepo:    deps.TaskStepRepo,
			TaskRunRepo: deps.TaskRunRepo,
		}))

	// Conversation routes require authentication (sessions are bound to individuals).
	aigc.Post("/conversation", withAuth(conversationHandler))
	// The GET version is for browser EventSource (EventSource only supports GET).
	aigc.Get("/conversation", withAuth(conversationHandler))

	// Step 3.6: card-confirmation endpoint (the driver of multi-agent chaining).
	// User clicks an interactive card → write back to intent_memory → advance FlowStage.
	aigc.Post("/agent/submit_clarification", withAuth(
		handler.SubmitClarificationHandler(handler.ClarificationDeps{
			Memory: deps.IntentMemory,
		})))

	// Internal endpoints: called in sequence by the agent tool chain (same-process loopback); no user JWT required.
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
		// The risk callback is owned by the risk domain; guard it with its own nil check (decoupled from the creation assembly).
		if deps.RiskHandler != nil {
			callback.Post("/risk", deps.RiskHandler.CallbackRisk)
		}
	}

	// Prometheus metrics endpoint (text format, encoded via prometheus/common expfmt).
	if deps.MetricsEnabled {
		app.Get("/metrics", MetricsHandler())
	}

	return app
}

// MetricsHandler exposes the default registry's metrics in Prometheus text format.
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
