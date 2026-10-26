package manager_test

import (
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/manager"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// task_type 非法值被拒（第 8 章 §8.4.3 测试断言还原）
func TestValidateGenerateResponse_BadTaskType(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"bad","task_arguments":"{}"}`)
	testx.False(t, vr.OK)
	testx.Equal(t, manager.StageSchemaValidate, vr.Stage)
	testx.Contains(t, vr.Message, "task_type")
}

// 非合法 JSON（Markdown 包裹）→ 修复 → 校验通过
func TestValidateGenerateResponse_RepairMarkdown(t *testing.T) {
	vr := manager.ValidateGenerateResponse("```json\n{\"thought\":\"t\",\"friendly_reply\":\"ok\",\"task_type\":\"only_reply\",\"task_arguments\":\"{}\"}\n```")
	testx.True(t, vr.OK)
	testx.Equal(t, "ok", vr.FriendlyReply)
}

// task_arguments 越权派发 idea_king → 拒绝（第 8 章 §8.4.3：选题只能走 idea_king_api，
// 不能走子任务投递；且 idea_king 不在产线展开的 plan 内）
func TestValidateGenerateResponse_AgentWhitelistReject(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"new_agents_plan","agents_pipeline":"mix_video","task_arguments":"{\"agents\":[{\"agent_id\":\"idea_king\",\"input\":\"start\"}]}"}`)
	testx.False(t, vr.OK)
	testx.Contains(t, vr.Message, "idea_king")
}

// 合法 new_agents_plan → 服务端按 LLM 填的 agents 翻译（§8.4.4：LLM 决定链）
func TestValidateGenerateResponse_ValidPlan(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"已派发","task_type":"new_agents_plan","agents_pipeline":"mix_video","task_arguments":"{\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"生成海报\"}]}"}`)
	testx.True(t, vr.OK)
	testx.NotNil(t, vr.Plan)
	testx.Equal(t, "new_agents_plan", vr.Plan.TaskType)
	// 链 = LLM 填的 agents（服务端不补产线标准 agent）
	testx.Len(t, vr.Plan.PlanSteps.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", vr.Plan.PlanSteps.Agents[0].AgentID)
	testx.Equal(t, "生成海报", vr.Plan.PlanSteps.Agents[0].Input)
}

// LLM 填完整的 mix_video 链 → 服务端翻译保留顺序 + 补缺失 input
func TestValidateGenerateResponse_ValidPlanFullChain(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"已派发","task_type":"new_agents_plan","agents_pipeline":"mix_video","task_arguments":"{\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"生成海报\"},{\"agent_id\":\"edit_master\",\"input\":\"\"}]}"}`)
	testx.True(t, vr.OK)
	testx.NotNil(t, vr.Plan)
	testx.Len(t, vr.Plan.PlanSteps.Agents, 2)
	testx.Equal(t, "gold_scriptwriter", vr.Plan.PlanSteps.Agents[0].AgentID)
	testx.Equal(t, "edit_master", vr.Plan.PlanSteps.Agents[1].AgentID)
	// edit_master 没填 input → 补产线默认
	testx.Equal(t, subtask.DefaultPipelineAgentInput(string(subtask.PipelineMixVideo), "edit_master"), vr.Plan.PlanSteps.Agents[1].Input)
}

// agents_pipeline 填了不存在的产线 → 拒绝 + 回显合法产线（§8.4.4 枚举约束）
func TestValidateGenerateResponse_UnknownPipelineReject(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"new_agents_plan","agents_pipeline":"no_such_pipeline","task_arguments":"{\"agents\":[]}"}`)
	testx.False(t, vr.OK)
	testx.Contains(t, vr.Message, "agents_pipeline")
	testx.Contains(t, vr.Message, "mix_video")
}

// new_agents_plan 缺 agents_pipeline → 拒绝（§8.4.4：必须选产线）
func TestValidateGenerateResponse_MissingPipelineReject(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"new_agents_plan","task_arguments":"{\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"x\"}]}"}`)
	testx.False(t, vr.OK)
	testx.Contains(t, vr.Message, "agents_pipeline")
}

// 兼容旧协议：agents_pipeline 填在 task_arguments 内层也能解析（fake 链路/老测试）
func TestValidateGenerateResponse_PipelineInTaskArguments(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"已派发","task_type":"new_agents_plan","task_arguments":"{\"agents_pipeline\":\"article\",\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"生成笔记\"}]}"}`)
	testx.True(t, vr.OK)
	testx.NotNil(t, vr.Plan)
	// article 链：idea_king → gold_scriptwriter；plan 从 gold_scriptwriter 起
	testx.Len(t, vr.Plan.PlanSteps.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", vr.Plan.PlanSteps.Agents[0].AgentID)
}

// LLM 给产线外 agent 填 input → 拒绝（§8.4.4：agent 链由服务端固定，LLM 只填产线内 agent）
func TestValidateGenerateResponse_AgentOutsidePipelineReject(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"new_agents_plan","agents_pipeline":"article","task_arguments":"{\"agents\":[{\"agent_id\":\"edit_master\",\"input\":\"x\"}]}"}`)
	testx.False(t, vr.OK)
	testx.Contains(t, vr.Message, "edit_master")
}

// task_arguments 被模型写成对象（非字符串）→ 容错处理
func TestValidateGenerateResponse_ObjectTaskArguments(t *testing.T) {
	vr := manager.ValidateGenerateResponse(`{"thought":"t","friendly_reply":"ok","task_type":"only_reply","task_arguments":{"foo":"bar"}}`)
	testx.True(t, vr.OK)
}

// RepairJSON 去 Markdown 包裹
func TestRepairJSON(t *testing.T) {
	out, ok := manager.RepairJSON("```json\n{\"a\":1}\n```")
	testx.True(t, ok)
	testx.JSONEq(t, `{"a":1}`, out)
}

// generate_response 定义注入产线枚举（§8.4.4：agents_pipeline 是顶层参数 + enum 硬约束）。
// 产线数据源在 subtask，manager 的 protocol 注入需自动跟随。
func TestGenerateResponseTool_InjectsPipelineEnum(t *testing.T) {
	def := manager.ManagerProtocolTools()[0]
	agentsPipeline, ok := def.Parameters.Properties["agents_pipeline"]
	testx.True(t, ok, "generate_response 应有顶层 agents_pipeline 参数")
	// 硬约束：enum 覆盖所有合法产线（模型只能从枚举里选）
	testx.ElementsMatch(t, subtask.ValidPipelineNames(), agentsPipeline.Enum)
	testx.Contains(t, agentsPipeline.Enum, string(subtask.PipelineMixVideo))
	testx.Contains(t, agentsPipeline.Enum, string(subtask.PipelineArticle))
}
