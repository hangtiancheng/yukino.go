package appkitllm

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"

// DefaultRealLLM 是全局可注入的真实 LLM（app.go 启动时若配置了 LLM 则设置）。
// 设置后 Run/RunWithOutput/RunResume 的 llmOr/planLLMOr 优先用它。
// 未设置 → 回退 FakeLLM/FakePlanLLM（Phase 3 行为不变）。
//
// ⚠️ 并发约束：仅在启动时（app.New）写入一次，之后被各 Runner goroutine 并发读。
// 不要在运行期或测试中写入——会构成数据竞争。
var DefaultRealLLM react.LLM

// DefaultManagerLLM 是管家 ReAct 专用真实 LLM（强制 tool_choice=required，第 8 章 §8.2.1）。
// 与 DefaultRealLLM（auto，子 Agent 用）分离：管家必须结构化输出，子 Agent 保持 auto。
//
// ⚠️ 并发约束：与 DefaultRealLLM 相同，仅在启动时写入一次。
var DefaultManagerLLM react.LLM
