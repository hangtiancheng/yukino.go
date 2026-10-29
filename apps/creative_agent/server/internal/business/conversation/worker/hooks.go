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

// TaskRunnerHooks are the business decision callbacks of the conversation worker (injected by the app wiring).
// The worker itself is a generic shell for "consuming Redis Stream + lock/CAS/heartbeat/persistence/ACK";
// business decisions (fast-path checks / manager strategy+prompt injection / HIL triggering) all live in hooks.
//
// When hooks is nil the consumer degrades to a consumption shell without business wiring (backward compatible:
// only queue consumption + persistence + ACK, no ReAct constraint chain).
type TaskRunnerHooks struct {
	// FastPath decides the fast path (skipped when nil). On a hit with handled=true the worker does not run ReAct.
	FastPath *fastpath.FastPath
	// Memory is the intent memory (HIL checkpoint / FlowStage). Checkpoint saving is skipped when nil.
	Memory *memory.IntentMemory
	// UseManagerStrategy: whether to inject the manager constraint-chain strategy + manager prompt (the app sets true when a real LLM is configured).
	UseManagerStrategy bool
	// ManagerPrepare is the manager prompt-injection function (used when UseManagerStrategy=true).
	// Defaults to manager.ManagerPrepare; the app may override it (e.g. in tests).
	ManagerPrepare func(q string) []react.Message
	// CheckpointSave is the callback saving the checkpoint on an HIL interrupt (defaults to SaveCheckpoint when nil and Memory!=nil).
	CheckpointSave func(sessionID string, cp *react.Checkpoint)
	// ShouldEnableHIL decides whether the query should enable the HIL breakpoint chain (injecting FakeHILStrategy).
	// This is the conversation trigger-word rule (e.g. the query contains "确认"). Disabled by default when nil.
	ShouldEnableHIL func(query string) bool
	// ThoughtRepo is the repository persisting a thought row per LLM round (thought persistence is skipped when nil).
	ThoughtRepo repository.AgentThoughtRepository
	// IdeaKing is the hotspot/topic retrieval provider (when nil the manager strategy falls back to MockProvider).
	IdeaKing ideaking.IdeaKingProvider
}

// ApplyReActSetup applies the business assembly (manager strategy/prompt + HIL injection) and registers onInterrupt.
// When UseManagerStrategy=true it injects ManagerStrategy + ManagerPrepare (the Task 3 regression fix point);
// whether RunWithOutput is used is decided by the caller (taskSubmitter wiring); this method does not judge that.
func (h *TaskRunnerHooks) ApplyReActSetup(runner *appkitrunner.SupervisorRunner, onInterrupt func(cp *react.Checkpoint)) {
	if h == nil {
		return
	}
	if h.UseManagerStrategy {
		// Manager constraint chain (forced function calling + tool whitelist + schema validation).
		// The IdeaKing provider is injected into the strategy (for hot-card direct emission) and the tool registry.
		runner.SetPlanStrategy(&manager.ManagerStrategy{IdeaKing: h.IdeaKing})
		runner.SetPlanTools(manager.NewManagerToolRegistry(h.IdeaKing).AsCoreTools())
		// Task 3 regression fix: the manager prompt must be re-injected — appkit/runner no longer carries business prompts;
		// the manager business injects the constraint-chain protocol via SetPrepare (generate_response / task_type / agents_pipeline).
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

// setupRunner is the Consumer's thin delegation to TaskRunnerHooks.ApplyReActSetup (no-op for nil hooks).
func (c *Consumer) setupRunner(runner *appkitrunner.SupervisorRunner, onInterrupt func(cp *react.Checkpoint)) {
	if c.hooks != nil {
		c.hooks.ApplyReActSetup(runner, onInterrupt)
	}
}

// fastPathOr returns the hooks-injected fast-path decider (nil when hooks is nil or FastPath is not injected).
func (c *Consumer) fastPathOr() *fastpath.FastPath {
	if c.hooks == nil {
		return nil
	}
	return c.hooks.FastPath
}

// checkpointLoader returns the callback reading the HIL checkpoint by sessionID (nil when hooks.Memory is nil).
func (c *Consumer) checkpointLoader() func(sessionID string) *react.Checkpoint {
	if c.hooks == nil || c.hooks.Memory == nil {
		return nil
	}
	return c.hooks.Memory.LoadCheckpoint
}

// checkpointSaver returns the callback saving the HIL checkpoint (hooks.CheckpointSave first,
// then hooks.Memory.SaveCheckpoint; when neither exists it returns nil and saving is silently skipped).
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

// checkpointClearer returns the callback clearing the HIL checkpoint (nil when hooks.Memory is nil).
func (c *Consumer) checkpointClearer() func(sessionID string) {
	if c.hooks == nil || c.hooks.Memory == nil {
		return nil
	}
	return c.hooks.Memory.ClearCheckpoint
}

// hilEnabled decides whether the HIL breakpoint chain should be enabled for the query (false when hooks.ShouldEnableHIL is nil).
func (c *Consumer) hilEnabled(query string) bool {
	if c.hooks == nil || c.hooks.ShouldEnableHIL == nil {
		return false
	}
	return c.hooks.ShouldEnableHIL(query)
}
