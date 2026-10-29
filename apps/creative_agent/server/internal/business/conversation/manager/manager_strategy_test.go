package manager_test

import (
	"context"
	"errors"
	"testing"

	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/manager"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// Constraint ①: GetTools offers only generate_response + idea_king_api
func TestManagerStrategy_GetTools(t *testing.T) {
	defs := manager.ManagerStrategy{}.GetTools(react.NewAgentContext("hi"))
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	testx.ElementsMatch(t, []string{"generate_response", "idea_king_api"}, names)
}

// Constraint ① fallback: no tool_calls → Retry
func TestManagerStrategy_NoToolCallsRetry(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{Content: "你好"}
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionRetry, d.Action)
	testx.Contains(t, d.Metadata["retry_message"].(string), "generate_response")
}

// Constraint ②: tool names outside the whitelist → Retry
func TestManagerStrategy_UnknownToolRetry(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "not_a_tool", Args: "{}"}},
	}
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionRetry, d.Action)
	testx.Contains(t, d.Metadata["retry_message"].(string), "工具名错误")
}

// Constraint ③: generate_response validation failure → Retry with a precise error
func TestManagerStrategy_InvalidArgsRetry(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "generate_response", Args: `{"task_type":"bad"}`}},
	}
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionRetry, d.Action)
	testx.Contains(t, d.Metadata["retry_message"].(string), "task_type")
}

// new_agents_plan passing validation → Terminal + a parsable plan_output passed through
func TestManagerStrategy_ValidPlanTerminal(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "generate_response", Args: `{"thought":"t","friendly_reply":"已派发","task_type":"new_agents_plan","agents_pipeline":"mix_video","task_arguments":"{\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"生成海报\"}]}"}`}},
	}
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionTerminal, d.Action)
	rawPlan, _ := d.Metadata["plan_output"].(string)
	testx.NotEmpty(t, rawPlan)
	plan := appkitrunner.ParsePlanOutput(rawPlan)
	testx.NotNil(t, plan, "plan_output should be parsable by ParsePlanOutput (top-level structure)")
	// Chain = the LLM-filled agents (the server does not add standard pipeline agents); the mock filled only gold_scriptwriter
	testx.Len(t, plan.PlanSteps.Agents, 1)
}

// idea_king_api: the strategy emits the hot card directly (Terminal + hot_card, no next LLM round)
func TestManagerStrategy_IdeaKingContinue(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "idea_king_api", Args: `{"query":"体育"}`}},
	}
	// No injected provider → MockProvider is used (returns topics with eids)
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionTerminal, d.Action, "idea_king_api should emit Terminal directly, not Continue")
	card, ok := d.Metadata["hot_card"].(*manager.HotCardData)
	testx.True(t, ok, "hot_card should be passed through")
	testx.NotEmpty(t, card.TitleData, "the hot card should carry a topic list")
	testx.Equal(t, "热点及选题卡", card.CardType)
	for _, ti := range card.TitleData {
		testx.NotEmpty(t, ti.Eid, "topics should carry an eid (echoed on card click to locate the hotspot)")
	}
}

// idea_king_api provider error → Terminal fallback copy
func TestManagerStrategy_IdeaKingError_Fallback(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "idea_king_api", Args: `{"query":"体育"}`}},
	}
	// Inject an always-failing provider → fallback copy
	d := (&manager.ManagerStrategy{IdeaKing: failIdeaKingProvider{}}).Decide(c)
	testx.Equal(t, react.ActionTerminal, d.Action)
	testx.Contains(t, d.Metadata["friendly_reply"].(string), "点子王")
}

// failIdeaKingProvider is a provider stub that always returns an error.
type failIdeaKingProvider struct{}

func (failIdeaKingProvider) SearchHotTopics(ctx context.Context, query string, count int) (*ideaking.HotTopicResult, error) {
	return nil, errors.New("injected: hot issue service down")
}

// TestRealLLMOutput_OnlyReply verifies that a real DeepSeek output format under the generate_response protocol
// (task_type=only_reply, task_arguments omitted) passes constraint ③ schema validation.
func TestRealLLMOutput_OnlyReply(t *testing.T) {
	// An actual response from the real DeepSeek deepseek-chat (tool_choice=required)
	args := `{"thought":"用户请求写产品介绍文案。","friendly_reply":"您好！请提供产品信息。","task_type":"only_reply"}`
	vr := manager.ValidateGenerateResponse(args)
	testx.True(t, vr.OK, "a real LLM's only_reply output should pass validation: %s", vr.Message)
	testx.Nil(t, vr.Plan, "only_reply should not produce a plan")
	testx.Equal(t, "您好！请提供产品信息。", vr.FriendlyReply)
}

// push_card: the strategy consumes task_arguments and emits the creation-feature card (Terminal + hot_card + image-text)
func TestManagerStrategy_PushCard_ImageText(t *testing.T) {
	c := react.NewAgentContext("根据xxx开始创作")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "generate_response", Args: `{"thought":"t","friendly_reply":"请选择创作功能","task_type":"push_card","task_arguments":"{\"card_type\":\"创作功能选择卡\",\"layout\":\"image-text\",\"label\":\"老板想用哪个创作功能\",\"prompt_items\":[{\"title\":\"AI成片\",\"text\":\"海量素材一键智能成片\"},{\"title\":\"AI笔记图\",\"text\":\"一句话做爆款图文笔记\"}]}"}`}},
	}
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionTerminal, d.Action)
	card, ok := d.Metadata["hot_card"].(*manager.HotCardData)
	testx.True(t, ok, "push_card should pass hot_card through")
	testx.Len(t, card.PromptItems, 2)
	testx.Equal(t, "AI成片", card.PromptItems[0].Title)
	testx.Equal(t, "mix_video", card.PromptItems[0].Pipeline, "AI成片 should map to the mix_video pipeline")
	testx.Equal(t, "AI笔记图", card.PromptItems[1].Title)
	testx.Equal(t, "article", card.PromptItems[1].Pipeline, "AI笔记图 should map to the article pipeline")
	testx.Equal(t, "image-text", card.Layout)
}
