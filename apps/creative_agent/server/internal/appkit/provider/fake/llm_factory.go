// Package appkitfake provides test stubs (Fake*) and the LLM factory fallback logic.
// It depends on appkit/provider/llm (the real eino LLM) and appkit/provider/tools (FakeTools), with no cycles.
package appkitfake

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/llm"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// DefaultLLM returns the real LLM (when base_url is configured) or falls back to FakeLLM.
// All engine injection points go through here: when unconfigured, behavior matches Phase 3 (FakeLLM) without breaking existing tests.
// When the eino ChatModel build fails (invalid config) it also falls back to FakeLLM, guaranteeing the demo always runs.
func DefaultLLM(cfg appkitllm.Config) react.LLM {
	if cfg.BaseURL != "" {
		if real, err := appkitllm.NewEinoLLM(cfg); err == nil {
			return real
		}
	}
	return &FakeLLM{}
}
