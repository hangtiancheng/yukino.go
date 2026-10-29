package appkitfake

import (
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// FakeStrategy decides the Action from the LLM response content.
//   - Has tool_calls → Continue
//   - Text contains "only_reply" → Terminal
//   - Otherwise → Retry
type FakeStrategy struct{}

func (FakeStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return []react.FunctionDef{
		{Name: "search", Description: "Search for information"},
	}
}

func (FakeStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse
	if len(resp.ToolCalls) > 0 {
		return &react.Decision{Action: react.ActionContinue}
	}
	if strings.Contains(resp.Content, "only_reply") {
		return &react.Decision{
			Action:   react.ActionTerminal,
			Metadata: map[string]any{"friendly_reply": extractFriendlyReply(resp.Content)},
		}
	}
	return &react.Decision{
		Action:   react.ActionRetry,
		Metadata: map[string]any{"retry_message": "请按 only_reply 格式输出。"},
	}
}

func extractFriendlyReply(content string) string {
	const key = `"friendly_reply":"`
	if i := strings.Index(content, key); i >= 0 {
		rest := content[i+len(key):]
		if j := strings.Index(rest, `"`); j >= 0 {
			return rest[:j]
		}
	}
	return content
}

// FakePlanStrategy is the decision strategy of the manager new_agents_plan chain.
//
//   - Has tool_calls → Continue (subtask-dispatch tool)
//   - Text contains new_agents_plan → Terminal (produce the plan)
//   - Otherwise → Retry
//
// The only difference from FakeStrategy: the Terminal criterion is new_agents_plan instead of only_reply.
type FakePlanStrategy struct{}

func (FakePlanStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return []react.FunctionDef{
		{Name: "dispatch_agent", Description: "Dispatch subtasks"},
	}
}

func (FakePlanStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse
	if len(resp.ToolCalls) > 0 {
		return &react.Decision{Action: react.ActionContinue}
	}
	if strings.Contains(resp.Content, "new_agents_plan") {
		return &react.Decision{
			Action: react.ActionTerminal,
			Metadata: map[string]any{
				"friendly_reply": extractFriendlyReply(resp.Content),
				// The full plan JSON is passed through to the engine's AgentResult.Ext for RunWithOutput to parse and dispatch subtasks
				"plan_output": resp.Content,
			},
		}
	}
	return &react.Decision{
		Action:   react.ActionRetry,
		Metadata: map[string]any{"retry_message": "请按 new_agents_plan 格式输出。"},
	}
}

// FakeHILStrategy is the decision strategy of the HIL breakpoint chain (Phase 4 4.2).
//
// Behavior: when the query contains the trigger word, the first tool-less round produces ActionInterrupt (push a card and wait for user action);
// the resume round (Runtime.Interrupted=true) terminates directly. Without the trigger word → standard Terminal.
type FakeHILStrategy struct{}

func (FakeHILStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return []react.FunctionDef{
		{Name: "search", Description: "Search for information"},
	}
}

func (FakeHILStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse
	if len(resp.ToolCalls) > 0 {
		return &react.Decision{Action: react.ActionContinue}
	}
	// Resume round → terminate directly
	if c.Runtime.Interrupted {
		return &react.Decision{
			Action:   react.ActionTerminal,
			Metadata: map[string]any{"friendly_reply": "好的，继续完成你的任务。"},
		}
	}
	// First run: contains the trigger word → Interrupt
	if strings.Contains(c.Query, "确认") {
		return &react.Decision{
			Action:   react.ActionInterrupt,
			Metadata: map[string]any{"friendly_reply": "我需要你确认一下这个脚本方案（HIL 断点）。"},
		}
	}
	return &react.Decision{
		Action:   react.ActionTerminal,
		Metadata: map[string]any{"friendly_reply": extractFriendlyReply(resp.Content)},
	}
}
