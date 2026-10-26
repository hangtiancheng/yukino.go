package subtask_test

import (
	"sort"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// 产线注册表：可选的产线名覆盖 mix_video / article，且排序稳定（Schema enum 需确定性）。
func TestValidPipelineNames(t *testing.T) {
	names := subtask.ValidPipelineNames()
	testx.Len(t, names, 2)
	testx.Contains(t, names, string(subtask.PipelineMixVideo))
	testx.Contains(t, names, string(subtask.PipelineArticle))
	// 排序稳定：enum 注入用，map 遍历顺序随机 → 必须确定
	testx.True(t, sort.StringsAreSorted(names), "ValidPipelineNames 应排序稳定")
}

// 读取产线模板：存在返回模板，不存在返回 error。
func TestGetPipelineTemplate(t *testing.T) {
	tpl, err := subtask.GetPipelineTemplate(string(subtask.PipelineMixVideo))
	testx.NoError(t, err)
	testx.Equal(t, string(subtask.PipelineMixVideo), tpl.PipelineName)

	_, err = subtask.GetPipelineTemplate("no_such_pipeline")
	testx.Error(t, err)
}

// GetPipelineAgents：返回产线的合法 agent 集合（含选题 agent idea_king）；未知产线报错。
func TestGetPipelineAgents(t *testing.T) {
	mix, err := subtask.GetPipelineAgents(subtask.PipelineMixVideo)
	testx.NoError(t, err)
	testx.Equal(t, []subtask.AgentID{
		subtask.AgentIdeaKing, subtask.AgentGoldScriptwriter, subtask.AgentEditMaster,
	}, mix)

	article, err := subtask.GetPipelineAgents(subtask.PipelineArticle)
	testx.NoError(t, err)
	testx.Equal(t, []subtask.AgentID{
		subtask.AgentIdeaKing, subtask.AgentGoldScriptwriter,
	}, article)

	_, err = subtask.GetPipelineAgents("no_such_pipeline")
	testx.Error(t, err)
}

// PipelineAgentTypes：产线 agent 链（唯一数据源 categoryToAgentMapping）。
func TestPipelineAgentTypes(t *testing.T) {
	types, err := subtask.PipelineAgentTypes(string(subtask.PipelineMixVideo))
	testx.NoError(t, err)
	testx.Equal(t, []subtask.AgentID{
		subtask.AgentIdeaKing, subtask.AgentGoldScriptwriter, subtask.AgentEditMaster,
	}, types)

	_, err = subtask.PipelineAgentTypes("no_such_pipeline")
	testx.Error(t, err)
}

// GetNextAgent：pipeline → agents 顺序映射（推进 / 链结束 / 未知产线 / 不在映射从第一个开始）。
func TestGetNextAgent_Mapping(t *testing.T) {
	// mix_video：idea_king → gold_scriptwriter → edit_master
	next, err := subtask.GetNextAgent(subtask.PipelineMixVideo, subtask.AgentIdeaKing)
	testx.NoError(t, err)
	testx.Equal(t, subtask.AgentGoldScriptwriter, next)

	next2, err := subtask.GetNextAgent(subtask.PipelineMixVideo, subtask.AgentGoldScriptwriter)
	testx.NoError(t, err)
	testx.Equal(t, subtask.AgentEditMaster, next2)

	// 已是最后一个 → 返回空（链结束）
	next3, err := subtask.GetNextAgent(subtask.PipelineMixVideo, subtask.AgentEditMaster)
	testx.NoError(t, err)
	testx.Empty(t, next3)

	// article：idea_king → gold_scriptwriter
	next4, err := subtask.GetNextAgent(subtask.PipelineArticle, subtask.AgentIdeaKing)
	testx.NoError(t, err)
	testx.Equal(t, subtask.AgentGoldScriptwriter, next4)

	// 未知 pipeline → error
	_, err = subtask.GetNextAgent("no_such_pipeline", subtask.AgentIdeaKing)
	testx.Error(t, err)

	// 当前 agent 不在映射 → 从第一个开始
	next5, err := subtask.GetNextAgent(subtask.PipelineMixVideo, "unknown_agent")
	testx.NoError(t, err)
	testx.Equal(t, subtask.AgentIdeaKing, next5)
}

// ExpandPipelinePlan：链 = LLM 填的 agents（服务端只翻译，不展开固定链）。
func TestExpandPipelinePlan_MixVideo(t *testing.T) {
	plan, err := subtask.ExpandPipelinePlan(string(subtask.PipelineMixVideo), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentGoldScriptwriter), Input: "生成海报脚本"},
		{AgentID: string(subtask.AgentEditMaster), Input: ""}, // 没填 input → 补产线默认
	})
	testx.NoError(t, err)
	testx.Len(t, plan, 2, "链由 LLM 决定，填几个就是几个")
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), plan[0].AgentID)
	testx.Equal(t, "生成海报脚本", plan[0].Input, "LLM 显式给 input → 直接用")
	testx.Equal(t, string(subtask.AgentEditMaster), plan[1].AgentID)
	testx.Equal(t, subtask.DefaultPipelineAgentInput(string(subtask.PipelineMixVideo), string(subtask.AgentEditMaster)), plan[1].Input, "未给 input 的 agent → 补产线默认指令")
}

// ExpandPipelinePlan：LLM 填的 input 缺失 → 补产线默认。
func TestExpandPipelinePlan_NoInput(t *testing.T) {
	plan, err := subtask.ExpandPipelinePlan(string(subtask.PipelineArticle), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentGoldScriptwriter), Input: ""},
	})
	testx.NoError(t, err)
	testx.Len(t, plan, 1)
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), plan[0].AgentID)
	testx.Equal(t, subtask.DefaultPipelineAgentInput(string(subtask.PipelineArticle), string(subtask.AgentGoldScriptwriter)), plan[0].Input)
}

// ExpandPipelinePlan：LLM 只填一个 agent → 链就一个（服务端不补产线标准 agent）。
func TestExpandPipelinePlan_LLMDecidesChain(t *testing.T) {
	plan, err := subtask.ExpandPipelinePlan(string(subtask.PipelineMixVideo), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentGoldScriptwriter), Input: "只写脚本"},
	})
	testx.NoError(t, err)
	testx.Len(t, plan, 1, "LLM 完全决定链，服务端不自动补 edit_master")
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), plan[0].AgentID)
}

// ExpandPipelinePlan：LLM 填 idea_king → 被剔除（走 idea_king_api 工具，不进 plan）。
func TestExpandPipelinePlan_SkipIdeaKing(t *testing.T) {
	plan, err := subtask.ExpandPipelinePlan(string(subtask.PipelineMixVideo), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentIdeaKing), Input: "选题"},
		{AgentID: string(subtask.AgentGoldScriptwriter), Input: "写脚本"},
	})
	testx.NoError(t, err)
	testx.Len(t, plan, 1)
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), plan[0].AgentID)
}

// ExpandPipelinePlan：未知产线 → error。
func TestExpandPipelinePlan_UnknownPipeline(t *testing.T) {
	_, err := subtask.ExpandPipelinePlan("no_such_pipeline", nil)
	testx.Error(t, err)
}

// ExpandPipelinePlan：LLM 只填 idea_king → 展开后为空 → error。
func TestExpandPipelinePlan_AllSkipped(t *testing.T) {
	_, err := subtask.ExpandPipelinePlan(string(subtask.PipelineMixVideo), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentIdeaKing), Input: "选题"},
	})
	testx.Error(t, err)
}

// DefaultPipelineAgentInput：产线中某 agent 的默认指令。
func TestDefaultPipelineAgentInput(t *testing.T) {
	script := subtask.DefaultPipelineAgentInput("mix_video", string(subtask.AgentGoldScriptwriter))
	testx.Contains(t, script, "脚本")
	testx.Contains(t, script, "mix_video")

	edit := subtask.DefaultPipelineAgentInput("mix_video", string(subtask.AgentEditMaster))
	testx.Contains(t, edit, "成片")

	other := subtask.DefaultPipelineAgentInput("mix_video", "other_agent")
	testx.Contains(t, other, "mix_video")
}
