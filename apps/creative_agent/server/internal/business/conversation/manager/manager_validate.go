package manager

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
)

// ValidateStage is the validation stage (chapter 8 §8.4.2 staged error reporting).
type ValidateStage int

const (
	// StageJSONParse: arguments are not valid JSON: clear them + Retry echoing the original arguments.
	StageJSONParse ValidateStage = iota
	// StageSchemaValidate: JSON is valid but violates the schema: keep the arguments + report precisely.
	StageSchemaValidate
)

// ValidateResult is the validation result of generate_response arguments.
type ValidateResult struct {
	Stage         ValidateStage
	OK            bool
	Message       string       // correction hint shown to the LLM (injected by ActionRetry)
	FriendlyReply string       // friendly reply when validation passes
	Plan          *PlanOutput  // plan parsed when task_type=new_agents_plan
	Card          *HotCardData // card parsed when task_type=push_card (creation-feature card, etc.)
}

// ValidTaskTypes is the valid task_type enum (chapter 8 §8.2.2).
var ValidTaskTypes = map[string]bool{
	"only_reply": true, "push_card": true, "new_agents_plan": true, "execute_agent": true,
}

// ValidateGenerateResponse validates the arguments of the generate_response tool (constraint ③).
//
// Flow (chapter 8 §8.4.2): RepairJSON → task_type enum validation →
// task_arguments parsing (string / object tolerant) → agent whitelist validation for new_agents_plan.
func ValidateGenerateResponse(arguments string) *ValidateResult {
	// 1. JSON repair (strip Markdown wrappers, etc.)
	repaired, ok := RepairJSON(arguments)
	if !ok {
		return &ValidateResult{
			Stage:   StageJSONParse,
			OK:      false,
			Message: "参数不是合法的 JSON 结构，请检查。原始参数：" + truncate(arguments, 200),
		}
	}

	// 2. Parse top-level fields (using a map instead of a struct: task_arguments may be a string or an object)
	var raw map[string]any
	if err := json.Unmarshal([]byte(repaired), &raw); err != nil {
		return &ValidateResult{
			Stage:   StageSchemaValidate,
			OK:      false,
			Message: "严格按 generate_response 参数定义输出：{thought, friendly_reply, task_type, task_arguments}",
		}
	}
	thought, _ := raw["thought"].(string)
	friendlyReply, _ := raw["friendly_reply"].(string)
	taskType, _ := raw["task_type"].(string)
	_ = thought

	// 3. task_type enum validation
	if !ValidTaskTypes[taskType] {
		return &ValidateResult{
			Stage:   StageSchemaValidate,
			OK:      false,
			Message: fmt.Sprintf("task_type 取值不合法：%q，必须是以下之一：only_reply / push_card / new_agents_plan / execute_agent", taskType),
		}
	}

	// 4. task_arguments: string / object tolerance
	taskArgsJSON := "{}"
	switch tv := raw["task_arguments"].(type) {
	case string:
		taskArgsJSON = tv
	case map[string]any:
		b, _ := json.Marshal(tv)
		taskArgsJSON = string(b)
	case nil:
		// Missing → "{}"
	default:
		return &ValidateResult{
			Stage:   StageSchemaValidate,
			OK:      false,
			Message: "task_arguments 必须是 JSON 对象或对象字符串",
		}
	}

	// 5. task_arguments must be a valid JSON object
	var taskArgs map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &taskArgs); err != nil {
		if r2, ok2 := RepairJSON(taskArgsJSON); ok2 {
			if err2 := json.Unmarshal([]byte(r2), &taskArgs); err2 != nil {
				return &ValidateResult{Stage: StageSchemaValidate, OK: false, Message: "task_arguments 必须是 JSON 对象字符串"}
			}
		} else {
			return &ValidateResult{Stage: StageSchemaValidate, OK: false, Message: "task_arguments 必须是 JSON 对象字符串"}
		}
	}

	// 6. new_agents_plan → validate the pipeline + validate that the LLM-filled agents belong to it → server-side chain translation
	//    agents_pipeline is a top-level generate_response parameter (hard schema enum constraint, §8.4.4)
	if taskType == "new_agents_plan" {
		pipelineName, _ := raw["agents_pipeline"].(string)
		plan, msg := validateAndBuildPlan(friendlyReply, pipelineName, taskArgs)
		if plan == nil {
			return &ValidateResult{Stage: StageSchemaValidate, OK: false, Message: msg}
		}
		return &ValidateResult{Stage: StageSchemaValidate, OK: true, FriendlyReply: friendlyReply, Plan: plan}
	}

	// 6.5 push_card → parse the card data (creation-feature card, etc.). task_arguments contains card_type +
	//     title_data ([{title,content,eid}]) or prompt_items ([{title,text,index,eid,imageUrl}]).
	if taskType == "push_card" {
		card := parsePushCard(taskArgs, friendlyReply)
		return &ValidateResult{Stage: StageSchemaValidate, OK: true, FriendlyReply: friendlyReply, Card: card}
	}

	return &ValidateResult{Stage: StageSchemaValidate, OK: true, FriendlyReply: friendlyReply}
}

// parsePushCard parses push_card's task_arguments → HotCardData.
// task_arguments structure (aligned with the reference):
//
//	{card_type, label, hotspot_data, title_data:[{title,content,eid}],
//	 prompt_items:[{title,text,index,eid,imageUrl}]}
//
// prompt_items takes priority (server-predefined cards may carry imageUrl); otherwise title_data is converted.
func parsePushCard(taskArgs map[string]any, friendlyReply string) *HotCardData {
	card := &HotCardData{
		CardType: "创作功能选择卡",
	}
	if ct, _ := taskArgs["card_type"].(string); ct != "" {
		card.CardType = ct
	}
	if l, _ := taskArgs["label"].(string); l != "" {
		card.Label = l
	}
	if hs, _ := taskArgs["hotspot_data"].(string); hs != "" {
		card.HotspotData = hs
	}
	if lay, _ := taskArgs["layout"].(string); lay != "" {
		card.Layout = lay
	}

	// prompt_items takes priority (may carry imageUrl)
	if pis, ok := taskArgs["prompt_items"].([]any); ok && len(pis) > 0 {
		for i, piRaw := range pis {
			pi, ok := piRaw.(map[string]any)
			if !ok {
				continue
			}
			item := PromptItem{
				Index: i,
			}
			if t, _ := pi["title"].(string); t != "" {
				item.Title = t
			}
			if t, _ := pi["text"].(string); t != "" {
				item.Text = t
			}
			if e, _ := pi["eid"].(string); e != "" {
				item.Eid = e
			}
			if u, _ := pi["imageUrl"].(string); u != "" {
				item.ImageURL = u
			}
			item.Pipeline = creationTypePipeline(item.Title)
			card.PromptItems = append(card.PromptItems, item)
		}
		return card
	}

	// title_data conversion
	if tds, ok := taskArgs["title_data"].([]any); ok {
		for i, tdRaw := range tds {
			td, ok := tdRaw.(map[string]any)
			if !ok {
				continue
			}
			item := PromptItem{Index: i}
			if t, _ := td["title"].(string); t != "" {
				item.Title = t
			}
			if t, _ := td["content"].(string); t != "" {
				item.Text = t
			}
			if e, _ := td["eid"].(string); e != "" {
				item.Eid = e
			}
			item.Pipeline = creationTypePipeline(item.Title)
			card.PromptItems = append(card.PromptItems, item)
		}
	}
	return card
}

// creationTypePipeline maps creation-feature names to pipelines (AI成片→mix_video / AI笔记图→article).
// Unrecognized names return empty (the frontend treats them as ordinary options).
func creationTypePipeline(title string) string {
	switch title {
	case "AI成片", "混剪成片", "AI成片混剪":
		return "mix_video"
	case "AI笔记图", "图文笔记", "AI笔记":
		return "article"
	}
	return ""
}

// validateAndBuildPlan validates new_agents_plan arguments and assembles the Plan.
// Returns (plan, correction hint): when plan is nil the hint is non-empty (precise error for ActionRetry injection).
func validateAndBuildPlan(friendlyReply, pipelineName string, taskArgs map[string]any) (*PlanOutput, string) {
	// 1. agents_pipeline: top-level generate_response parameter (hard schema enum constraint, §8.4.4).
	//    Legacy protocol compatibility: when the top level is empty, fall back to task_arguments.agents_pipeline (fake chain / old tests).
	//    A nonexistent key → precise error (the enum is a model-layer constraint; server validation remains the final defense)
	if pipelineName == "" {
		pipelineName, _ = taskArgs["agents_pipeline"].(string)
	}
	if pipelineName == "" {
		return nil, "new_agents_plan 需要 agents_pipeline（generate_response 顶层参数）：从服务端产线模板选键（" + listValidPipelines() + "），再填 agents 数组决定链"
	}
	if _, err := subtask.GetPipelineTemplate(pipelineName); err != nil {
		return nil, pipelineErrorHint + listValidPipelines() + "（你填了 " + pipelineName + "）"
	}

	// 2. Validate the LLM-filled input agents: they must belong to the selected pipeline, preventing overreach assembly
	//    (§8.4.4: the LLM decides how to assemble the chain, but every agent must be within the selected pipeline — preventing
	//     the LLM from stuffing overreach agents like image_painter into the mix_video pipeline; topic selection idea_king
	//     goes through the idea_king_api tool and is exempt)
	llmAgents := parseLLMAgents(taskArgs)
	for _, a := range llmAgents {
		if a.AgentID == "" {
			continue // entries with input but no agent_id → ignored by the server, not an error
		}
		if !pipelineContainsAgent(pipelineName, a.AgentID) {
			return nil, fmt.Sprintf("agent_id %q 不在产线 %s 的 agent 集合内（可选："+listValidPipelines()+"的 chain agent，选题 idea_king 走 idea_king_api 工具不进 plan）", a.AgentID, pipelineName)
		}
	}

	// 3. Server-side translation: chain = the LLM-filled agents (drop idea_king, fill missing inputs); the fixed chain is not expanded
	steps, err := subtask.ExpandPipelinePlan(pipelineName, llmAgents)
	if err != nil {
		return nil, "pipeline 展开失败：" + err.Error()
	}
	return &PlanOutput{
		TaskType:       "new_agents_plan",
		FriendlyReply:  friendlyReply,
		AgentsPipeline: pipelineName,
		PlanSteps:      PlanStepsPayload{Agents: steps},
	}, ""
}

// parseLLMAgents extracts the LLM-filled agent list (agent_id + input) from task_arguments.
// Compatible with two shapes:
//   - New protocol: the agents list (the LLM fills only agents within the pipeline and their inputs)
//   - Legacy protocol: plan_steps.agents (compatible with the Phase 3 fake LLM / old tests; validation-failure paths match the new protocol)
func parseLLMAgents(taskArgs map[string]any) []PlanAgentPayload {
	out := make([]PlanAgentPayload, 0)
	if ps, ok := taskArgs["plan_steps"].(map[string]any); ok {
		if agents, ok := ps["agents"].([]any); ok {
			for _, a := range agents {
				if m, ok := a.(map[string]any); ok {
					out = append(out, PlanAgentPayload{
						AgentID: asString(m["agent_id"]),
						Input:   asString(m["input"]),
					})
				}
			}
		}
	}
	if agents, ok := taskArgs["agents"].([]any); ok {
		for _, a := range agents {
			if m, ok := a.(map[string]any); ok {
				out = append(out, PlanAgentPayload{
					AgentID: asString(m["agent_id"]),
					Input:   asString(m["input"]),
				})
			}
		}
	}
	return out
}

// pipelineContainsAgent checks whether agent_id is in the pipeline's valid agent set.
// idea_king is exempt: although it heads the pipeline chain, it goes through the idea_king_api tool and does not enter the plan (§8.4.3).
func pipelineContainsAgent(pipelineName, agentID string) bool {
	agentTypes, err := subtask.PipelineAgentTypes(pipelineName)
	if err != nil {
		return false
	}
	for _, at := range agentTypes {
		if string(at) == agentID {
			return agentID != string(subtask.AgentIdeaKing)
		}
	}
	return false
}

// asString safely fetches a string field (nil / non-string always yields an empty string).
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// RepairJSON performs lightweight JSON repair (chapter 8 §8.4.2):
//  1. Strip ```json ... ``` Markdown wrappers
//  2. Trim surrounding whitespace
//  3. Validate with json.Valid
func RepairJSON(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if json.Valid([]byte(s)) {
		return s, true
	}
	return "", false
}

// truncate cuts a string to n characters (for error echo, avoiding floods).
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
