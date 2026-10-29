package manager_test

import (
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/manager"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// Invalid task_type values are rejected (chapter 8 §8.4.3 test assertions restored)
func TestValidateGenerateResponse_BadTaskType(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"bad","task_arguments":"{}"}`)
	testx.False(t, vr.OK)
	testx.Equal(t, manager.StageSchemaValidate, vr.Stage)
	testx.Contains(t, vr.Message, "task_type")
}

// Invalid JSON (Markdown wrapper) → repaired → validation passes
func TestValidateGenerateResponse_RepairMarkdown(t *testing.T) {
	vr := manager.ValidateGenerateResponse("```json\n{\"thought\":\"t\",\"friendly_reply\":\"ok\",\"task_type\":\"only_reply\",\"task_arguments\":\"{}\"}\n```")
	testx.True(t, vr.OK)
	testx.Equal(t, "ok", vr.FriendlyReply)
}

// task_arguments dispatching idea_king overreach → rejected (chapter 8 §8.4.3: topic selection may only use idea_king_api,
// not subtask dispatch; and idea_king is not in the pipeline-expanded plan)
func TestValidateGenerateResponse_AgentWhitelistReject(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"new_agents_plan","agents_pipeline":"mix_video","task_arguments":"{\"agents\":[{\"agent_id\":\"idea_king\",\"input\":\"start\"}]}"}`)
	testx.False(t, vr.OK)
	testx.Contains(t, vr.Message, "idea_king")
}

// Valid new_agents_plan → the server translates per the LLM-filled agents (§8.4.4: the LLM decides the chain)
func TestValidateGenerateResponse_ValidPlan(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"已派发","task_type":"new_agents_plan","agents_pipeline":"mix_video","task_arguments":"{\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"生成海报\"}]}"}`)
	testx.True(t, vr.OK)
	testx.NotNil(t, vr.Plan)
	testx.Equal(t, "new_agents_plan", vr.Plan.TaskType)
	// Chain = the LLM-filled agents (the server does not add the pipeline's standard agents)
	testx.Len(t, vr.Plan.PlanSteps.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", vr.Plan.PlanSteps.Agents[0].AgentID)
	testx.Equal(t, "生成海报", vr.Plan.PlanSteps.Agents[0].Input)
}

// The LLM fills the complete mix_video chain → server translation preserves order + fills missing inputs
func TestValidateGenerateResponse_ValidPlanFullChain(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"已派发","task_type":"new_agents_plan","agents_pipeline":"mix_video","task_arguments":"{\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"生成海报\"},{\"agent_id\":\"edit_master\",\"input\":\"\"}]}"}`)
	testx.True(t, vr.OK)
	testx.NotNil(t, vr.Plan)
	testx.Len(t, vr.Plan.PlanSteps.Agents, 2)
	testx.Equal(t, "gold_scriptwriter", vr.Plan.PlanSteps.Agents[0].AgentID)
	testx.Equal(t, "edit_master", vr.Plan.PlanSteps.Agents[1].AgentID)
	// edit_master has no input → filled with the pipeline default
	testx.Equal(t, subtask.DefaultPipelineAgentInput(string(subtask.PipelineMixVideo), "edit_master"), vr.Plan.PlanSteps.Agents[1].Input)
}

// agents_pipeline filled with a nonexistent pipeline → rejected + valid pipelines echoed (§8.4.4 enum constraint)
func TestValidateGenerateResponse_UnknownPipelineReject(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"new_agents_plan","agents_pipeline":"no_such_pipeline","task_arguments":"{\"agents\":[]}"}`)
	testx.False(t, vr.OK)
	testx.Contains(t, vr.Message, "agents_pipeline")
	testx.Contains(t, vr.Message, "mix_video")
}

// new_agents_plan missing agents_pipeline → rejected (§8.4.4: a pipeline must be chosen)
func TestValidateGenerateResponse_MissingPipelineReject(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"new_agents_plan","task_arguments":"{\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"x\"}]}"}`)
	testx.False(t, vr.OK)
	testx.Contains(t, vr.Message, "agents_pipeline")
}

// Legacy protocol compatibility: agents_pipeline inside task_arguments also parses (fake chain / old tests)
func TestValidateGenerateResponse_PipelineInTaskArguments(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"已派发","task_type":"new_agents_plan","task_arguments":"{\"agents_pipeline\":\"article\",\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"生成笔记\"}]}"}`)
	testx.True(t, vr.OK)
	testx.NotNil(t, vr.Plan)
	// article chain: idea_king → gold_scriptwriter; the plan starts at gold_scriptwriter
	testx.Len(t, vr.Plan.PlanSteps.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", vr.Plan.PlanSteps.Agents[0].AgentID)
}

// The LLM filling input for an agent outside the pipeline → rejected (§8.4.4: the agent chain is bounded by the server; the LLM fills only in-pipeline agents)
func TestValidateGenerateResponse_AgentOutsidePipelineReject(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"new_agents_plan","agents_pipeline":"article","task_arguments":"{\"agents\":[{\"agent_id\":\"edit_master\",\"input\":\"x\"}]}"}`)
	testx.False(t, vr.OK)
	testx.Contains(t, vr.Message, "edit_master")
}

// task_arguments written by the model as an object (not a string) → handled tolerantly
func TestValidateGenerateResponse_ObjectTaskArguments(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"only_reply","task_arguments":{"foo":"bar"}}`)
	testx.True(t, vr.OK)
}

// RepairJSON strips Markdown wrappers
func TestRepairJSON(t *testing.T) {
	out, ok := manager.RepairJSON("```json\n{\"a\":1}\n```")
	testx.True(t, ok)
	testx.JSONEq(t, `{"a":1}`, out)
}

// The generate_response definition injects the pipeline enum (§8.4.4: agents_pipeline is a top-level parameter + hard enum constraint).
// The pipeline data source lives in subtask; the manager's protocol injection must follow automatically.
func TestGenerateResponseTool_InjectsPipelineEnum(t *testing.T) {
	def := manager.ManagerProtocolTools()[0]
	agentsPipeline, ok := def.Parameters.Properties["agents_pipeline"]
	testx.True(t, ok, "generate_response should have the top-level agents_pipeline parameter")
	// Hard constraint: the enum covers all valid pipelines (the model can only pick from the enum)
	testx.ElementsMatch(t, subtask.ValidPipelineNames(), agentsPipeline.Enum)
	testx.Contains(t, agentsPipeline.Enum, string(subtask.PipelineMixVideo))
	testx.Contains(t, agentsPipeline.Enum, string(subtask.PipelineArticle))
}
