package manager

import (
	"encoding/json"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
)

// ManagerStrategy 是管家（Supervisor）的决策策略（第 8 章约束链）。
//
//   - 约束① 强制 Function Calling：GetTools 只给 generate_response + idea_king_api，
//     无 tool_calls 直接 Retry（防御兜底）。
//   - 约束② 工具名白名单：tool_calls 的 name 必须在策略工具内，否则 Retry。
//   - 约束③ Schema 校验：generate_response 参数走 ValidateGenerateResponse。
//   - 约束④ 失败闭环：所有失败 → ActionRetry 注入纠错提示（引擎最多重试 2 次）。
//
// task_type=new_agents_plan 校验通过 → Terminal，透传顶层 PlanOutput JSON
// （plan_steps 提到顶层，供 RunWithOutput 的 ParsePlanOutput 解析投递）。
//
// 热点卡（Phase 5）：idea_king_api 工具被拦截后 strategy 直出——
// 调外部热点服务，Terminal + Metadata.hot_card（不进下一轮 LLM），
// 由 Runner 渲染成 ui-block-start 热点卡。对齐原始实现 handleIdeaKingAPI。
type ManagerStrategy struct {
	// IdeaKing 是热点/选题检索 provider。nil 时用 MockProvider（未配置热点服务回退）。
	IdeaKing ideaking.IdeaKingProvider
}

// GetTools 返回管家可用的策略工具（约束①）。
func (ManagerStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return ManagerProtocolTools()
}

// Decide 决策管家下一步 Action。
func (s *ManagerStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse

	// 约束①兜底：无 tool_calls → Retry
	if len(resp.ToolCalls) == 0 {
		return &react.Decision{
			Action: react.ActionRetry,
			Metadata: map[string]any{
				"retry_message": "你的回复没有调用任何工具，导致和 server 端通信错误。如果需要和 server 端通信，请使用 `generate_response` 工具按消息协议通信！",
			},
		}
	}

	// 逐个校验 tool_calls
	var retryMsg string
	for _, tc := range resp.ToolCalls {
		switch tc.Name {
		case "idea_king_api":
			// 热点/选题：strategy 直出（调外部热点服务 → Terminal + hot_card，不进下一轮 LLM）。
			return s.handleIdeaKing(c, tc)
		case "generate_response":
			vr := ValidateGenerateResponse(tc.Args)
			if !vr.OK {
				retryMsg = vr.Message
				continue
			}
			if vr.Plan != nil {
				// new_agents_plan：Terminal + 透传顶层 plan_output
				planJSON, _ := json.Marshal(vr.Plan)
				return &react.Decision{
					Action: react.ActionTerminal,
					Metadata: map[string]any{
						"friendly_reply": vr.Plan.FriendlyReply,
						"plan_output":    string(planJSON),
					},
				}
			}
			if vr.Card != nil {
				// push_card：Terminal + 透传卡数据（创作功能卡等），runner 渲染 ui-block-start
				return &react.Decision{
					Action: react.ActionTerminal,
					Metadata: map[string]any{
						"friendly_reply": vr.FriendlyReply,
						"hot_card":       vr.Card,
					},
				}
			}
			// 其它 task_type（only_reply / execute_agent）：Terminal
			return &react.Decision{
				Action:   react.ActionTerminal,
				Metadata: map[string]any{"friendly_reply": vr.FriendlyReply},
			}
		default:
			// 约束②：工具名白名单
			if !ManagerToolNameAllowed(tc.Name) {
				retryMsg = "工具名错误！请检查工具名，修正后重新调用！可用工具：generate_response、idea_king_api"
			}
		}
	}
	if retryMsg != "" {
		return &react.Decision{Action: react.ActionRetry, Metadata: map[string]any{"retry_message": retryMsg}}
	}
	// 全是白名单内非协议工具（如 idea_king_api）→ Continue 执行工具，结果当 observation
	return &react.Decision{Action: react.ActionContinue}
}

// ideaKing 返回热点检索 provider（nil 时用 MockProvider 兜底，demo 恒可跑）。
func (s *ManagerStrategy) ideaKing() ideaking.IdeaKingProvider {
	if s.IdeaKing != nil {
		return s.IdeaKing
	}
	return ideaking.NewMockProvider()
}

// handleIdeaKing 处理 idea_king_api 工具调用（strategy 直出热点卡）。
// 对齐原始实现 DujiaStrategy.handleIdeaKingAPI：
//   - 解析 query/hotspot_count → 调外部热点服务
//   - 成功：ActionTerminal + Metadata.hot_card（*HotCardData，Runner 渲染成 ui-block-start 卡），
//     不进下一轮 LLM（热点卡数据从外部服务来，LLM 不参与组织）
//   - 失败：Terminal 兜底文案
func (s *ManagerStrategy) handleIdeaKing(c *react.AgentContext, tc react.ToolCall) *react.Decision {
	params := ParseArgs(tc.Args)
	query := ArgString(params, "query", "")
	count := ArgInt(params, "hotspot_count", 3)

	res, err := s.ideaKing().SearchHotTopics(c.Ctx, query, count)
	if err != nil || res == nil || len(res.TitleData) == 0 {
		return &react.Decision{
			Action: react.ActionTerminal,
			Metadata: map[string]any{
				"friendly_reply": "点子王出了些小问题，没有获取到有效的热点数据，换个问法试试～",
			},
		}
	}

	return &react.Decision{
		Action: react.ActionTerminal,
		Metadata: map[string]any{
			"friendly_reply": "", // 卡帧自带文本，不发空终态 text-delta
			"hot_card": &HotCardData{
				CardType:    "热点及选题卡",
				HotspotData: res.HotspotData,
				TitleData:   res.TitleData,
			},
		},
	}
}
