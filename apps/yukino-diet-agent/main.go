// Command diet-agent serves the Go migration of the Java Diet-agent:
// a multi-agent diet recommendation assistant built on yukino_http and
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

	"github.com/hangtiancheng/yukino.go/apps/diet-agent/internal/api"
	"github.com/hangtiancheng/yukino.go/apps/diet-agent/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/diet-agent/internal/diet"
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

	store, err := diet.NewStore(cfg.Database)
	if err != nil {
		logger.Error("connect database failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()

	models, err := diet.NewModelSet(cfg.LLM.APIKeyValue(), cfg.LLM.BaseURLValue(), cfg.LLM.MainModelValue(), cfg.LLM.LightModelValue())
	if err != nil {
		logger.Error("init LLM models failed", "error", err)
		os.Exit(1)
	}
	agents := diet.NewAgentSet(models, diet.PromptLoader{}, cfg.LLM.CallTimeout())

	traceService := diet.NewTraceService(store)
	sessionService := diet.NewSessionService(store, cfg.Session.MaxHistoryTurns)
	sessionStateService := diet.NewSessionStateService(store)
	slotOptionService := diet.NewSlotOptionService(store)
	mealService := diet.NewMealService(store, slotOptionService)
	mealSearchService := diet.NewMealSearchService(mealService)
	mealPlanService := diet.NewMealPlanService(mealSearchService)
	intentAgentService := diet.NewIntentAgentService(agents, traceService, slotOptionService)
	clarifyAgentService := diet.NewClarifyAgentService(agents, traceService)
	recommendResponseService := diet.NewRecommendResponseAgentService(agents, traceService)
	planResponseService := diet.NewPlanResponseAgentService(agents, traceService)
	judgeService := diet.NewEvaluationJudgeService(agents, traceService)
	evaluationService := diet.NewEvaluationService(traceService, store, judgeService)
	feedbackService := diet.NewFeedbackService(store)

	orchestrator := diet.NewOrchestrator(
		sessionService,
		sessionStateService,
		intentAgentService,
		clarifyAgentService,
		diet.SlotMergeService{},
		mealSearchService,
		diet.MealRankService{},
		recommendResponseService,
		mealPlanService,
		planResponseService,
		mealService,
		diet.RiskGuardService{},
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
		logger.Info("diet-agent listening", "addr", server.Addr,
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
