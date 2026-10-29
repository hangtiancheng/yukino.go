package manager

import (
	"encoding/json"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
)

// ManagerStrategy is the decision strategy of the manager (Supervisor) (the chapter 8 constraint chain).
//
//   - Constraint ① forced function calling: GetTools only offers generate_response + idea_king_api;
//     no tool_calls → immediate Retry (defensive fallback).
//   - Constraint ② tool-name whitelist: the name in tool_calls must be among the strategy tools, otherwise Retry.
//   - Constraint ③ schema validation: generate_response arguments go through ValidateGenerateResponse.
//   - Constraint ④ failure closure: every failure → ActionRetry injects a correction hint (the engine retries at most twice).
//
// task_type=new_agents_plan passing validation → Terminal, passing the top-level PlanOutput JSON through
// (plan_steps is lifted to the top level for RunWithOutput's ParsePlanOutput to parse and dispatch).
//
// Hot-topic card (Phase 5): after the idea_king_api tool is intercepted, the strategy emits directly —
// it calls the external hotspot service, Terminal + Metadata.hot_card (without another LLM round),
// and the Runner renders it into a ui-block-start hot-topic card. Aligned with the original handleIdeaKingAPI.
type ManagerStrategy struct {
	// IdeaKing is the hotspot/topic retrieval provider. When nil, MockProvider is used (fallback when no hotspot service is configured).
	IdeaKing ideaking.IdeaKingProvider
}

// GetTools returns the strategy tools available to the manager (constraint ①).
func (ManagerStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return ManagerProtocolTools()
}

// Decide determines the manager's next Action.
func (s *ManagerStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse

	// Constraint ① fallback: no tool_calls → Retry
	if len(resp.ToolCalls) == 0 {
		return &react.Decision{
			Action: react.ActionRetry,
			Metadata: map[string]any{
				"retry_message": "你的回复没有调用任何工具，导致和 server 端通信错误。如果需要和 server 端通信，请使用 `generate_response` 工具按消息协议通信！",
			},
		}
	}

	// Validate tool_calls one by one
	var retryMsg string
	for _, tc := range resp.ToolCalls {
		switch tc.Name {
		case "idea_king_api":
			// Hotspot/topic: the strategy emits directly (call the external hotspot service → Terminal + hot_card, no next LLM round).
			return s.handleIdeaKing(c, tc)
		case "generate_response":
			vr := ValidateGenerateResponse(tc.Args)
			if !vr.OK {
				retryMsg = vr.Message
				continue
			}
			if vr.Plan != nil {
				// new_agents_plan: Terminal + pass the top-level plan_output through
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
				// push_card: Terminal + pass the card data through (creation-feature card, etc.); the runner renders ui-block-start
				return &react.Decision{
					Action: react.ActionTerminal,
					Metadata: map[string]any{
						"friendly_reply": vr.FriendlyReply,
						"hot_card":       vr.Card,
					},
				}
			}
			// Other task_types (only_reply / execute_agent): Terminal
			return &react.Decision{
				Action:   react.ActionTerminal,
				Metadata: map[string]any{"friendly_reply": vr.FriendlyReply},
			}
		default:
			// Constraint ②: tool-name whitelist
			if !ManagerToolNameAllowed(tc.Name) {
				retryMsg = "工具名错误！请检查工具名，修正后重新调用！可用工具：generate_response、idea_king_api"
			}
		}
	}
	if retryMsg != "" {
		return &react.Decision{Action: react.ActionRetry, Metadata: map[string]any{"retry_message": retryMsg}}
	}
	// All whitelisted non-protocol tools (e.g. idea_king_api) → Continue executing the tools; results become observations
	return &react.Decision{Action: react.ActionContinue}
}

// ideaKing returns the hotspot retrieval provider (falls back to MockProvider when nil, so the demo always runs).
func (s *ManagerStrategy) ideaKing() ideaking.IdeaKingProvider {
	if s.IdeaKing != nil {
		return s.IdeaKing
	}
	return ideaking.NewMockProvider()
}

// handleIdeaKing handles the idea_king_api tool call (the strategy emits the hot-topic card directly).
// Aligned with the original DujiaStrategy.handleIdeaKingAPI:
//   - Parse query/hotspot_count → call the external hotspot service
//   - Success: ActionTerminal + Metadata.hot_card (*HotCardData, rendered by the Runner into a ui-block-start card);
//     no next LLM round (hot-card data comes from the external service; the LLM does not organize it)
//   - Failure: Terminal fallback copy
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
			"friendly_reply": "", // the card frames carry their own text; no empty terminal text-delta
			"hot_card": &HotCardData{
				CardType:    "热点及选题卡",
				HotspotData: res.HotspotData,
				TitleData:   res.TitleData,
			},
		},
	}
}
