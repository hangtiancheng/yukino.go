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

// 约束①：GetTools 只给 generate_response + idea_king_api
func TestManagerStrategy_GetTools(t *testing.T) {
	defs := manager.ManagerStrategy{}.GetTools(react.NewAgentContext("hi"))
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	testx.ElementsMatch(t, []string{"generate_response", "idea_king_api"}, names)
}

// 约束①兜底：无 tool_calls → Retry
func TestManagerStrategy_NoToolCallsRetry(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{Content: "你好"}
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionRetry, d.Action)
	testx.Contains(t, d.Metadata["retry_message"].(string), "generate_response")
}

// 约束②：白名单外工具名 → Retry
func TestManagerStrategy_UnknownToolRetry(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "not_a_tool", Args: "{}"}},
	}
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionRetry, d.Action)
	testx.Contains(t, d.Metadata["retry_message"].(string), "工具名错误")
}

// 约束③：generate_response 校验失败 → Retry 精准报错
func TestManagerStrategy_InvalidArgsRetry(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "generate_response", Args: `{"task_type":"bad"}`}},
	}
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionRetry, d.Action)
	testx.Contains(t, d.Metadata["retry_message"].(string), "task_type")
}

// new_agents_plan 校验通过 → Terminal + 透传可解析的 plan_output
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
	testx.NotNil(t, plan, "plan_output 应能被 ParsePlanOutput 解析（顶层结构）")
	// 链 = LLM 填的 agents（服务端不补产线标准 agent），mock 只填了 gold_scriptwriter
	testx.Len(t, plan.PlanSteps.Agents, 1)
}

// idea_king_api：strategy 直出热点卡（Terminal + hot_card，不进下一轮 LLM）
func TestManagerStrategy_IdeaKingContinue(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "idea_king_api", Args: `{"query":"体育"}`}},
	}
	// 未注入 provider → 用 MockProvider（返回带 eid 的选题）
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionTerminal, d.Action, "idea_king_api 应直出 Terminal，不再 Continue")
	card, ok := d.Metadata["hot_card"].(*manager.HotCardData)
	testx.True(t, ok, "应透传 hot_card")
	testx.NotEmpty(t, card.TitleData, "热点卡应有选题列表")
	testx.Equal(t, "热点及选题卡", card.CardType)
	for _, ti := range card.TitleData {
		testx.NotEmpty(t, ti.Eid, "选题应带 eid（用户点卡回传定位热点）")
	}
}

// idea_king_api provider 出错 → Terminal 兜底文案
func TestManagerStrategy_IdeaKingError_Fallback(t *testing.T) {
	c := react.NewAgentContext("hi")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "idea_king_api", Args: `{"query":"体育"}`}},
	}
	// 注入总是失败的 provider → 兜底文案
	d := (&manager.ManagerStrategy{IdeaKing: failIdeaKingProvider{}}).Decide(c)
	testx.Equal(t, react.ActionTerminal, d.Action)
	testx.Contains(t, d.Metadata["friendly_reply"].(string), "点子王")
}

// failIdeaKingProvider 是总是返回错误的 provider 桩。
type failIdeaKingProvider struct{}

func (failIdeaKingProvider) SearchHotTopics(ctx context.Context, query string, count int) (*ideaking.HotTopicResult, error) {
	return nil, errors.New("injected: hot issue service down")
}

// TestRealLLMOutput_OnlyReply 验证真实 DeepSeek 在 generate_response 协议下的
// 实际输出格式（task_type=only_reply，省略 task_arguments）能通过约束③ Schema 校验。
func TestRealLLMOutput_OnlyReply(t *testing.T) {
	// 来自真实 DeepSeek deepseek-chat 的实际响应（tool_choice=required）
	args := `{"thought":"用户请求写产品介绍文案。","friendly_reply":"您好！请提供产品信息。","task_type":"only_reply"}`
	vr := manager.ValidateGenerateResponse(args)
	testx.True(t, vr.OK, "真实 LLM 的 only_reply 输出应通过校验: %s", vr.Message)
	testx.Nil(t, vr.Plan, "only_reply 不应产出 plan")
	testx.Equal(t, "您好！请提供产品信息。", vr.FriendlyReply)
}

// push_card：strategy 消费 task_arguments 出创作功能卡（Terminal + hot_card + image-text）
func TestManagerStrategy_PushCard_ImageText(t *testing.T) {
	c := react.NewAgentContext("根据xxx开始创作")
	c.Runtime.LastResponse = &react.LLMResponse{
		ToolCalls: []react.ToolCall{{ID: "c1", Name: "generate_response", Args: `{"thought":"t","friendly_reply":"请选择创作功能","task_type":"push_card","task_arguments":"{\"card_type\":\"创作功能选择卡\",\"layout\":\"image-text\",\"label\":\"老板想用哪个创作功能\",\"prompt_items\":[{\"title\":\"AI成片\",\"text\":\"海量素材一键智能成片\"},{\"title\":\"AI笔记图\",\"text\":\"一句话做爆款图文笔记\"}]}"}`}},
	}
	d := (&manager.ManagerStrategy{}).Decide(c)
	testx.Equal(t, react.ActionTerminal, d.Action)
	card, ok := d.Metadata["hot_card"].(*manager.HotCardData)
	testx.True(t, ok, "push_card 应透传 hot_card")
	testx.Len(t, card.PromptItems, 2)
	testx.Equal(t, "AI成片", card.PromptItems[0].Title)
	testx.Equal(t, "mix_video", card.PromptItems[0].Pipeline, "AI成片 应映射 mix_video 产线")
	testx.Equal(t, "AI笔记图", card.PromptItems[1].Title)
	testx.Equal(t, "article", card.PromptItems[1].Pipeline, "AI笔记图 应映射 article 产线")
	testx.Equal(t, "image-text", card.Layout)
}
