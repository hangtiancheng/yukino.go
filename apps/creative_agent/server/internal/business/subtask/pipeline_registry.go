package subtask

import (
	"fmt"
	"sort"
)

// ============ Pipeline registry: the LLM decides the chain; the server only translates (chapter 8 §8.4.4) ============
//
// Aligned with the original project creative-gravity: the LLM decides how to assemble the chain in the task_arguments.agents array
// (order/composition entirely up to the LLM); the server only "translates" — validating agents are within the pipeline, filling missing inputs,
// and dropping the topic agent (idea_king goes through the idea_king_api tool and does not enter the plan). The server does not expand a fixed chain.
//
// agents_pipeline's responsibility narrows to "picking a pipeline": hard schema enum constraint + in-pipeline validation guarantee every
// LLM-filled agent belongs to the selected pipeline (preventing overreach). The pipeline's valid agent set is defined by
// core.categoryToAgentMapping (single source of truth); the fast-task path's GetNextAgent reuses the same one.

// PipelineTemplate describes the fixed agent chain of one business pipeline.
//
// Aligned with the reference system's creative-gravity/apps/dujia/pipeline_registry.go managedPipelines:
// the replica reuses executor.categoryToAgentMapping as the single source of truth;
// only LLM-perceivable metadata (Description) is held here, avoiding two drifting agent-chain definitions.
type PipelineTemplate struct {
	// PipelineName is the pipeline name (the managedPipelines key, also the generate_response enum value).
	PipelineName string
	// Description is the one-line pipeline description (injected into the prompt, helping the LLM pick the right pipeline).
	Description string
}

// managedPipelines are the pipeline templates selectable by the manager LLM (chapter 8 §8.4.4).
//
// The order is the schema enum order; the LLM filling a nonexistent key → ValidateGenerateResponse reports precisely + Retry.
// AgentTypes are not defined here — they are read uniformly from executor.categoryToAgentMapping,
// guaranteeing that "the manager picking a pipeline" and "GetNextAgent advancing after card confirmation" use the same agent chain.
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

// ValidPipelineNames returns all selectable pipeline names (for schema enum injection + whitelist validation).
// Sorting guarantees a stable enum order (map iteration order is random; schema enums need determinism).
func ValidPipelineNames() []string {
	out := make([]string, 0, len(managedPipelines))
	for name := range managedPipelines {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// GetPipelineTemplate reads a pipeline template; returns (nil, err) when absent.
func GetPipelineTemplate(name string) (*PipelineTemplate, error) {
	t, ok := managedPipelines[name]
	if !ok {
		return nil, fmt.Errorf("unknown pipeline: %s", name)
	}
	return &t, nil
}

// PipelineAgentTypes returns the valid agent set of a pipeline (from core.categoryToAgentMapping).
// Used for in-pipeline validation (pipelineContainsAgent): every LLM-filled agent must belong to the selected pipeline.
func PipelineAgentTypes(name string) ([]AgentID, error) {
	return GetPipelineAgents(Pipeline(name))
}

// planAgentCapacity is the capacity cap of the plan agent chain (defending against abnormal templates blowing up ReAct).
const planAgentCapacity = 20

// ExpandPipelinePlan translates the LLM-chosen pipeline + LLM-filled agents into plan_steps.agents.
//
// Aligned with the original project's ConvertToPlanSteps (apps/dujia/hooks.go): the LLM decides the chain;
// the server only translates:
//   - chain = the LLM-filled agents array (order/composition entirely up to the LLM; the server does not expand a fixed chain)
//   - drop the topic agent (idea_king): it goes through the idea_king_api tool and does not enter the plan
//   - fill agents without input with the pipeline default instruction (corresponding to the original project's
//     server-side FormatInvokeMessage template rendering for downstream agents' invoke_message)
//
// Pipeline validation (pipelineContainsAgent) is done by the caller before entering — this function only translates.
func ExpandPipelinePlan(pipelineName string, llmAgents []PlanAgentPayload) ([]PlanAgentPayload, error) {
	if _, err := GetPipelineTemplate(pipelineName); err != nil {
		return nil, err
	}

	plan := make([]PlanAgentPayload, 0, len(llmAgents))
	for _, a := range llmAgents {
		if a.AgentID == "" {
			continue // entries with input but no agent_id → ignored by the server
		}
		if a.AgentID == string(AgentIdeaKing) {
			continue // the topic agent goes through the idea_king_api tool; it does not enter the plan
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

// DefaultPipelineAgentInput returns the default instruction of an agent in a pipeline.
// Currently all pipelines share one set (matching the fake environment's capabilities); real business can customize per pipeline here.
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
