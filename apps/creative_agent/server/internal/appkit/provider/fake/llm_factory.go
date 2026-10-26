// Package appkitfake 提供测试桩（Fake*）与 LLM 工厂回退逻辑。
// 依赖 appkit/provider/llm（eino 真实 LLM）与 appkit/provider/tools（FakeTools），无环。
package appkitfake

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/llm"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// DefaultLLM 根据配置返回真实 LLM（配置了 base_url）或回退 FakeLLM。
// 引擎注入点统一走这里：未配置时行为与 Phase 3 一致（FakeLLM），不破坏现有测试。
// eino ChatModel 构建失败（配置非法）时同样回退 FakeLLM，保证 demo 恒可跑。
func DefaultLLM(cfg appkitllm.Config) react.LLM {
	if cfg.BaseURL != "" {
		if real, err := appkitllm.NewEinoLLM(cfg); err == nil {
			return real
		}
	}
	return &FakeLLM{}
}
