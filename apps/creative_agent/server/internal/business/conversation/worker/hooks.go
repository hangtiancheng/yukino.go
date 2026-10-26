package worker

import (
	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/fastpath"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/manager"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
)

// TaskRunnerHooks 是对话 worker 的业务决策回调（由 app 装配注入）。
// worker 本身是"消费 Redis Stream + 锁/CAS/心跳/落库/ACK"的通用壳，
// 业务决策（快路径判断 / 管家策略+prompt 注入 / HIL 触发）都在 hooks 里。
//
// hooks 为 nil 时 consumer 退化为无业务接线的消费壳（保持向后兼容：
// 只有队列消费 + 落库 + ACK，不跑 ReAct 约束链）。
type TaskRunnerHooks struct {
	// FastPath 快路径判定（nil 时跳过）。命中 handled=true 时 worker 不跑 ReAct。
	FastPath *fastpath.FastPath
	// Memory 意图记忆（HIL checkpoint / FlowStage）。nil 时跳过 checkpoint 保存。
	Memory *memory.IntentMemory
	// UseManagerStrategy 是否注入管家约束链策略 + 管家 prompt（配置了真实 LLM 时 app 设为 true）。
	UseManagerStrategy bool
	// ManagerPrepare 是管家 prompt 注入函数（UseManagerStrategy=true 时用它）。
	// 默认 manager.ManagerPrepare；app 可覆盖（如测试）。
	ManagerPrepare func(q string) []react.Message
	// CheckpointSave 是 HIL 中断时保存 checkpoint 的回调（nil 且 Memory!=nil 时默认 SaveCheckpoint）。
	CheckpointSave func(sessionID string, cp *react.Checkpoint)
	// ShouldEnableHIL 判断 query 是否应启用 HIL 断点链路（注入 FakeHILStrategy）。
	// 这是对话触发词规则（如 query 含"确认"）。nil 时默认不启用。
	ShouldEnableHIL func(query string) bool
	// ThoughtRepo 是每轮 LLM 落 thought 表的仓库（nil 时跳过 thought 落库）。
	ThoughtRepo repository.AgentThoughtRepository
	// IdeaKing 是热点/选题检索 provider（nil 时管家策略用 MockProvider 兜底）。
	IdeaKing ideaking.IdeaKingProvider
}

// ApplyReActSetup 应用业务装配（管家策略/prompt + HIL 注入），并注册 onInterrupt。
// UseManagerStrategy=true 时注入 ManagerStrategy + ManagerPrepare（Task 3 回归修复点）；
// 是否走 RunWithOutput 由调用方（taskSubmitter 接线）决定，本方法不做此判断。
func (h *TaskRunnerHooks) ApplyReActSetup(runner *appkitrunner.SupervisorRunner, onInterrupt func(cp *react.Checkpoint)) {
	if h == nil {
		return
	}
	if h.UseManagerStrategy {
		// 管家约束链（强制 Function Calling + 工具白名单 + Schema 校验）。
		// IdeaKing provider 注入策略（热点卡 strategy 直出用）与工具注册表。
		runner.SetPlanStrategy(&manager.ManagerStrategy{IdeaKing: h.IdeaKing})
		runner.SetPlanTools(manager.NewManagerToolRegistry(h.IdeaKing).AsCoreTools())
		// Task 3 回归修复：管家 prompt 必须重新注入——appkit/runner 不再背业务 prompt，
		// 由管家业务经 SetPrepare 注入约束链协议（generate_response / task_type / agents_pipeline）。
		prepare := h.ManagerPrepare
		if prepare == nil {
			prepare = manager.ManagerPrepare
		}
		runner.SetPrepare(prepare)
	}
	if onInterrupt != nil {
		runner.SetOnInterrupt(onInterrupt)
	}
}

// setupRunner 是 Consumer 对 TaskRunnerHooks.ApplyReActSetup 的薄委托（nil hooks 无操作）。
func (c *Consumer) setupRunner(runner *appkitrunner.SupervisorRunner, onInterrupt func(cp *react.Checkpoint)) {
	if c.hooks != nil {
		c.hooks.ApplyReActSetup(runner, onInterrupt)
	}
}

// fastPathOr 返回 hooks 注入的快路径判定器（hooks 为 nil 或未注入 FastPath 时返回 nil）。
func (c *Consumer) fastPathOr() *fastpath.FastPath {
	if c.hooks == nil {
		return nil
	}
	return c.hooks.FastPath
}

// checkpointLoader 返回按 sessionID 读 HIL checkpoint 的回调（hooks.Memory nil 时返回 nil）。
func (c *Consumer) checkpointLoader() func(sessionID string) *react.Checkpoint {
	if c.hooks == nil || c.hooks.Memory == nil {
		return nil
	}
	return c.hooks.Memory.LoadCheckpoint
}

// checkpointSaver 返回保存 HIL checkpoint 的回调（优先 hooks.CheckpointSave，
// 其次 hooks.Memory.SaveCheckpoint；均无则返回 nil 静默跳过）。
func (c *Consumer) checkpointSaver() func(sessionID string, cp *react.Checkpoint) {
	if c.hooks == nil {
		return nil
	}
	if c.hooks.CheckpointSave != nil {
		return c.hooks.CheckpointSave
	}
	if c.hooks.Memory != nil {
		return c.hooks.Memory.SaveCheckpoint
	}
	return nil
}

// checkpointClearer 返回清除 HIL checkpoint 的回调（hooks.Memory nil 时返回 nil）。
func (c *Consumer) checkpointClearer() func(sessionID string) {
	if c.hooks == nil || c.hooks.Memory == nil {
		return nil
	}
	return c.hooks.Memory.ClearCheckpoint
}

// hilEnabled 判断是否应为该 query 启用 HIL 断点链路（hooks.ShouldEnableHIL nil 时 false）。
func (c *Consumer) hilEnabled(query string) bool {
	if c.hooks == nil || c.hooks.ShouldEnableHIL == nil {
		return false
	}
	return c.hooks.ShouldEnableHIL(query)
}
