package subtask

import (
	"fmt"
	"sort"
)

// ============ pipeline registry：LLM 决定链，服务端只翻译（第 8 章 §8.4.4）============
//
// 与原始项目 creative-gravity 对齐：LLM 在 task_arguments.agents 数组里决定链怎么装配
// （顺序/组成全归 LLM），服务端只做「翻译」——校验 agent 在产线内、补缺失的 input、
// 剔除选题 agent（idea_king 走 idea_king_api 工具不进 plan）。服务端不展开固定链。
//
// agents_pipeline 的职责收窄为「选产线」：Schema enum 硬约束 + 产线内校验，保证 LLM
// 填的每个 agent 都属于所选产线（防越权）。产线的合法 agent 集合由
// core.categoryToAgentMapping 定义（唯一数据源），快任务路径 GetNextAgent 复用同一份。

// PipelineTemplate 描述一条业务产线的固定 agent 链。
//
// 与真实系统 creative-gravity/apps/dujia/pipeline_registry.go 的 managedPipelines
// 对齐：复刻版直接复用 executor.categoryToAgentMapping 作为唯一数据源，
// 这里仅持有 LLM 可感知的元信息（Description），避免两套 agent 链定义漂移。
type PipelineTemplate struct {
	// PipelineName 产线名（managedPipelines 的键，也是 generate_response 的枚举值）。
	PipelineName string
	// Description 产线一句话说明（注入 prompt，帮 LLM 选对产线）。
	Description string
}

// managedPipelines 管家 LLM 可选的产线模板（第 8 章 §8.4.4）。
//
// 顺序即 Schema 枚举顺序；LLM 填不存在的键 → ValidateGenerateResponse 精准报错 + Retry。
// AgentTypes 不在这里定义——统一从 executor.categoryToAgentMapping 读，
// 保证「管家选产线」与「点卡确认后 GetNextAgent 推进」用的是同一条 agent 链。
var managedPipelines = map[string]PipelineTemplate{
	string(PipelineMixVideo): {
		PipelineName: string(PipelineMixVideo),
		Description:  "混剪成片",
	},
	string(PipelineArticle): {
		PipelineName: string(PipelineArticle),
		Description:  "图文笔记",
	},
}

// ValidPipelineNames 返回所有可选的产线名（Schema 枚举注入 + 白名单校验用）。
// 排序保证 enum 顺序稳定（map 遍历顺序随机，Schema enum 需确定性）。
func ValidPipelineNames() []string {
	out := make([]string, 0, len(managedPipelines))
	for name := range managedPipelines {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// GetPipelineTemplate 读取产线模板，不存在返回 (nil, err)。
func GetPipelineTemplate(name string) (*PipelineTemplate, error) {
	t, ok := managedPipelines[name]
	if !ok {
		return nil, fmt.Errorf("unknown pipeline: %s", name)
	}
	return &t, nil
}

// PipelineAgentTypes 返回产线的合法 agent 集合（来自 core.categoryToAgentMapping）。
// 用于产线内校验（pipelineContainsAgent）：LLM 填的每个 agent 必须属于所选产线。
func PipelineAgentTypes(name string) ([]AgentID, error) {
	return GetPipelineAgents(Pipeline(name))
}

// planAgentCapacity 是 plan agent 链的容量上限（防御异常模板炸掉 ReAct）。
const planAgentCapacity = 20

// ExpandPipelinePlan 把 LLM 选的产线 + 填的 agents 翻译成 plan_steps.agents。
//
// 与原始项目 ConvertToPlanSteps（apps/dujia/hooks.go）对齐：LLM 决定链，
// 服务端只做翻译：
//   - 链 = LLM 填的 agents 数组（顺序/组成全归 LLM，服务端不展开固定链）
//   - 剔除选题 agent（idea_king）：它走 idea_king_api 工具，不进 plan
//   - 给没填 input 的 agent 补产线默认指令（对应原始项目后续 agent 的
//     invoke_message 由服务端 FormatInvokeMessage 渲染模板填充）
//
// 产线校验（pipelineContainsAgent）由调用方在进入前完成——这里只负责翻译。
func ExpandPipelinePlan(pipelineName string, llmAgents []PlanAgentPayload) ([]PlanAgentPayload, error) {
	if _, err := GetPipelineTemplate(pipelineName); err != nil {
		return nil, err
	}

	plan := make([]PlanAgentPayload, 0, len(llmAgents))
	for _, a := range llmAgents {
		if a.AgentID == "" {
			continue // 只有 input 无 agent_id 的项 → 服务端忽略
		}
		if a.AgentID == string(AgentIdeaKing) {
			continue // 选题 agent 走 idea_king_api 工具，不进 plan
		}
		if len(plan) >= planAgentCapacity {
			return nil, fmt.Errorf("pipeline %s 的 agent 链超过 %d 上限", pipelineName, planAgentCapacity)
		}
		input := a.Input
		if input == "" {
			input = DefaultPipelineAgentInput(pipelineName, a.AgentID)
		}
		plan = append(plan, PlanAgentPayload{AgentID: a.AgentID, Input: input})
	}
	if len(plan) == 0 {
		return nil, fmt.Errorf("pipeline %s 的 agents 为空（选题 idea_king 走 idea_king_api 工具，不进 plan）", pipelineName)
	}
	return plan, nil
}

// DefaultPipelineAgentInput 返回产线中某 agent 的默认指令。
// 目前所有产线共用同一套（与 fake 环境能力匹配）；真实业务可在此按产线定制。
func DefaultPipelineAgentInput(pipelineName, agentID string) string {
	switch agentID {
	case string(AgentGoldScriptwriter):
		return fmt.Sprintf("请围绕用户意图产出完整脚本（产线 %s）", pipelineName)
	case string(AgentEditMaster):
		return fmt.Sprintf("请根据脚本产出成片（产线 %s）", pipelineName)
	default:
		return fmt.Sprintf("请完成用户意图中属于你的部分（产线 %s）", pipelineName)
	}
}
