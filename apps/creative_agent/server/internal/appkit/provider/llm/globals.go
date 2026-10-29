package appkitllm

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"

// DefaultRealLLM is the globally injectable real LLM (set by app.go at startup when an LLM is configured).
// Once set, llmOr/planLLMOr of Run/RunWithOutput/RunResume prefer it.
// Unset → falls back to FakeLLM/FakePlanLLM (Phase 3 behavior unchanged).
//
// ⚠️ Concurrency constraint: written once at startup (app.New), then read concurrently by Runner goroutines.
// Do not write at runtime or in tests — that would be a data race.
var DefaultRealLLM react.LLM

// DefaultManagerLLM is the manager-ReAct-dedicated real LLM (forced tool_choice=required, chapter 8 §8.2.1).
// Separate from DefaultRealLLM (auto, for sub-agents): the manager must output structured results; sub-agents stay auto.
//
// ⚠️ Concurrency constraint: same as DefaultRealLLM, written once at startup only.
var DefaultManagerLLM react.LLM
