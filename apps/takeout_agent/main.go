// Command takeout_agent serves the Go migration of the Java takeout_agent:
// a multi-agent takeout recommendation assistant built on yukino_http and
// AgentScope Go (DashScope).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/takeout_agent/internal/api"
	"github.com/hangtiancheng/yukino.go/apps/takeout_agent/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/takeout_agent/internal/takeout"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := config.LoadDotEnv(".env"); err != nil {
		logger.Warn("load .env failed", "error", err)
	}

	cfg, err := config.Load(config.ConfigPath("config.yaml"))
	if err != nil {
		logger.Error("load config failed", "error", err)
		os.Exit(1)
	}

	store, err := takeout.NewStore(cfg.Database)
	if err != nil {
		logger.Error("connect database failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()

	models, err := takeout.NewModelSet(cfg.LLM.APIKeyValue(), cfg.LLM.BaseURLValue(), cfg.LLM.MainModelValue(), cfg.LLM.LightModelValue())
	if err != nil {
		logger.Error("init LLM models failed", "error", err)
		os.Exit(1)
	}
	agents := takeout.NewAgentSet(models, takeout.PromptLoader{}, cfg.LLM.CallTimeout())

	traceService := takeout.NewTraceService(store)
	sessionService := takeout.NewSessionService(store, cfg.Session.MaxHistoryTurns)
	sessionStateService := takeout.NewSessionStateService(store)
	slotOptionService := takeout.NewSlotOptionService(store)
	mealService := takeout.NewMealService(store, slotOptionService)
	mealSearchService := takeout.NewMealSearchService(mealService)
	mealPlanService := takeout.NewMealPlanService(mealSearchService)
	intentAgentService := takeout.NewIntentAgentService(agents, traceService, slotOptionService)
	clarifyAgentService := takeout.NewClarifyAgentService(agents, traceService)
	recommendResponseService := takeout.NewRecommendResponseAgentService(agents, traceService)
	planResponseService := takeout.NewPlanResponseAgentService(agents, traceService)
	judgeService := takeout.NewEvaluationJudgeService(agents, traceService)
	evaluationService := takeout.NewEvaluationService(traceService, store, judgeService)
	feedbackService := takeout.NewFeedbackService(store)

	orchestrator := takeout.NewOrchestrator(
		sessionService,
		sessionStateService,
		intentAgentService,
		clarifyAgentService,
		takeout.SlotMergeService{},
		mealSearchService,
		takeout.MealRankService{},
		recommendResponseService,
		mealPlanService,
		planResponseService,
		mealService,
		takeout.RiskGuardService{},
		traceService,
	)

	app := api.NewApp(&api.Services{
		Orchestrator: orchestrator,
		Sessions:     sessionService,
		Meals:        mealService,
		SlotOptions:  slotOptionService,
		Evaluations:  evaluationService,
		Feedback:     feedbackService,
		Traces:       traceService,
	}, cfg.Static.Dir)

	server := &http.Server{
		Addr:              cfg.Server.Addr(),
		Handler:           app,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("takeout_agent listening", "addr", server.Addr,
			"mainModel", cfg.LLM.MainModelValue(), "lightModel", cfg.LLM.LightModelValue())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}
