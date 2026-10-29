package subtask_test

import (
	"sort"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// Pipeline registry: the selectable pipeline names cover mix_video / article and are stably sorted (schema enums need determinism).
func TestValidPipelineNames(t *testing.T) {
	names := subtask.ValidPipelineNames()
	testx.Len(t, names, 2)
	testx.Contains(t, names, string(subtask.PipelineMixVideo))
	testx.Contains(t, names, string(subtask.PipelineArticle))
	// Stable sort: used for enum injection; map iteration order is random → must be deterministic
	testx.True(t, sort.StringsAreSorted(names), "ValidPipelineNames should be stably sorted")
}

// Reading pipeline templates: existing returns the template; missing returns an error.
func TestGetPipelineTemplate(t *testing.T) {
	tpl, err := subtask.GetPipelineTemplate(string(subtask.PipelineMixVideo))
	testx.NoError(t, err)
	testx.Equal(t, string(subtask.PipelineMixVideo), tpl.PipelineName)

	_, err = subtask.GetPipelineTemplate("no_such_pipeline")
	testx.Error(t, err)
}

// GetPipelineAgents: returns the pipeline's valid agent set (including the topic agent idea_king); unknown pipelines error.
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

// PipelineAgentTypes: the pipeline agent chain (single source of truth categoryToAgentMapping).
func TestPipelineAgentTypes(t *testing.T) {
	types, err := subtask.PipelineAgentTypes(string(subtask.PipelineMixVideo))
	testx.NoError(t, err)
	testx.Equal(t, []subtask.AgentID{
		subtask.AgentIdeaKing, subtask.AgentGoldScriptwriter, subtask.AgentEditMaster,
	}, types)

	_, err = subtask.PipelineAgentTypes("no_such_pipeline")
	testx.Error(t, err)
}

// GetNextAgent: pipeline → agents ordered mapping (advance / chain end / unknown pipeline / not in mapping starts from the first).
func TestGetNextAgent_Mapping(t *testing.T) {
	// mix_video: idea_king → gold_scriptwriter → edit_master
	next, err := subtask.GetNextAgent(subtask.PipelineMixVideo, subtask.AgentIdeaKing)
	testx.NoError(t, err)
	testx.Equal(t, subtask.AgentGoldScriptwriter, next)

	next2, err := subtask.GetNextAgent(subtask.PipelineMixVideo, subtask.AgentGoldScriptwriter)
	testx.NoError(t, err)
	testx.Equal(t, subtask.AgentEditMaster, next2)

	// Already the last → returns empty (chain end)
	next3, err := subtask.GetNextAgent(subtask.PipelineMixVideo, subtask.AgentEditMaster)
	testx.NoError(t, err)
	testx.Empty(t, next3)

	// article: idea_king → gold_scriptwriter
	next4, err := subtask.GetNextAgent(subtask.PipelineArticle, subtask.AgentIdeaKing)
	testx.NoError(t, err)
	testx.Equal(t, subtask.AgentGoldScriptwriter, next4)

	// Unknown pipeline → error
	_, err = subtask.GetNextAgent("no_such_pipeline", subtask.AgentIdeaKing)
	testx.Error(t, err)

	// Current agent not in the mapping → start from the first
	next5, err := subtask.GetNextAgent(subtask.PipelineMixVideo, "unknown_agent")
	testx.NoError(t, err)
	testx.Equal(t, subtask.AgentIdeaKing, next5)
}

// ExpandPipelinePlan: chain = the LLM-filled agents (the server only translates; it does not expand a fixed chain).
func TestExpandPipelinePlan_MixVideo(t *testing.T) {
	plan, err := subtask.ExpandPipelinePlan(string(subtask.PipelineMixVideo), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentGoldScriptwriter), Input: "生成海报脚本"},
		{AgentID: string(subtask.AgentEditMaster), Input: ""}, // no input → filled with the pipeline default
	})
	testx.NoError(t, err)
	testx.Len(t, plan, 2, "the chain is decided by the LLM; however many are filled is how many there are")
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), plan[0].AgentID)
	testx.Equal(t, "生成海报脚本", plan[0].Input, "the LLM explicitly gave input → used directly")
	testx.Equal(t, string(subtask.AgentEditMaster), plan[1].AgentID)
	testx.Equal(t, subtask.DefaultPipelineAgentInput(string(subtask.PipelineMixVideo), string(subtask.AgentEditMaster)), plan[1].Input, "agents without input → filled with the pipeline default instruction")
}

// ExpandPipelinePlan: missing LLM-filled input → filled with the pipeline default.
func TestExpandPipelinePlan_NoInput(t *testing.T) {
	plan, err := subtask.ExpandPipelinePlan(string(subtask.PipelineArticle), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentGoldScriptwriter), Input: ""},
	})
	testx.NoError(t, err)
	testx.Len(t, plan, 1)
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), plan[0].AgentID)
	testx.Equal(t, subtask.DefaultPipelineAgentInput(string(subtask.PipelineArticle), string(subtask.AgentGoldScriptwriter)), plan[0].Input)
}

// ExpandPipelinePlan: the LLM fills only one agent → the chain has one (the server does not add standard pipeline agents).
func TestExpandPipelinePlan_LLMDecidesChain(t *testing.T) {
	plan, err := subtask.ExpandPipelinePlan(string(subtask.PipelineMixVideo), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentGoldScriptwriter), Input: "只写脚本"},
	})
	testx.NoError(t, err)
	testx.Len(t, plan, 1, "the LLM fully decides the chain; the server does not auto-add edit_master")
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), plan[0].AgentID)
}

// ExpandPipelinePlan: the LLM fills idea_king → dropped (it goes through the idea_king_api tool, not the plan).
func TestExpandPipelinePlan_SkipIdeaKing(t *testing.T) {
	plan, err := subtask.ExpandPipelinePlan(string(subtask.PipelineMixVideo), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentIdeaKing), Input: "选题"},
		{AgentID: string(subtask.AgentGoldScriptwriter), Input: "写脚本"},
	})
	testx.NoError(t, err)
	testx.Len(t, plan, 1)
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), plan[0].AgentID)
}

// ExpandPipelinePlan: unknown pipeline → error.
func TestExpandPipelinePlan_UnknownPipeline(t *testing.T) {
	_, err := subtask.ExpandPipelinePlan("no_such_pipeline", nil)
	testx.Error(t, err)
}

// ExpandPipelinePlan: the LLM fills only idea_king → empty after expansion → error.
func TestExpandPipelinePlan_AllSkipped(t *testing.T) {
	_, err := subtask.ExpandPipelinePlan(string(subtask.PipelineMixVideo), []subtask.PlanAgentPayload{
		{AgentID: string(subtask.AgentIdeaKing), Input: "选题"},
	})
	testx.Error(t, err)
}

// DefaultPipelineAgentInput: the default instruction of an agent in a pipeline.
func TestDefaultPipelineAgentInput(t *testing.T) {
	script := subtask.DefaultPipelineAgentInput("mix_video", string(subtask.AgentGoldScriptwriter))
	testx.Contains(t, script, "脚本")
	testx.Contains(t, script, "mix_video")

	edit := subtask.DefaultPipelineAgentInput("mix_video", string(subtask.AgentEditMaster))
	testx.Contains(t, edit, "成片")

	other := subtask.DefaultPipelineAgentInput("mix_video", "other_agent")
	testx.Contains(t, other, "mix_video")
}
