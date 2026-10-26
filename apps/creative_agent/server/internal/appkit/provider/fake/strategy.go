package appkitfake

import (
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// FakeStrategy 根据 LLM 响应内容判定 Action。
//   - 有 tool_calls → Continue
//   - 文本含 "only_reply" → Terminal
//   - 否则 → Retry
type FakeStrategy struct{}

func (FakeStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return []react.FunctionDef{
		{Name: "search", Description: "搜索信息"},
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

// FakePlanStrategy 是管家 new_agents_plan 链路的决策策略。
//
//   - 有 tool_calls → Continue（派发子任务工具）
//   - 文本含 new_agents_plan → Terminal（产出计划）
//   - 否则 → Retry
//
// 与 FakeStrategy 的唯一区别：Terminal 判据是 new_agents_plan 而非 only_reply。
type FakePlanStrategy struct{}

func (FakePlanStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return []react.FunctionDef{
		{Name: "dispatch_agent", Description: "派发子任务"},
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
				// 完整计划 JSON 透传给引擎 AgentResult.Ext，供 RunWithOutput 解析投递子任务
				"plan_output": resp.Content,
			},
		}
	}
	return &react.Decision{
		Action:   react.ActionRetry,
		Metadata: map[string]any{"retry_message": "请按 new_agents_plan 格式输出。"},
	}
}

// FakeHILStrategy 是 HIL 断点链路的决策策略（Phase 4 4.2）。
//
// 行为：query 含触发词时，在第一个无工具轮次产出 ActionInterrupt（推卡片等用户操作）；
// 恢复轮次（Runtime.Interrupted=true）直接 Terminal。query 无触发词 → 标准 Terminal。
type FakeHILStrategy struct{}

func (FakeHILStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return []react.FunctionDef{
		{Name: "search", Description: "搜索信息"},
	}
}

func (FakeHILStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse
	if len(resp.ToolCalls) > 0 {
		return &react.Decision{Action: react.ActionContinue}
	}
	// resume 恢复轮次 → 直接 Terminal
	if c.Runtime.Interrupted {
		return &react.Decision{
			Action:   react.ActionTerminal,
			Metadata: map[string]any{"friendly_reply": "好的，继续完成你的任务。"},
		}
	}
	// 首次：含触发词 → Interrupt
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
