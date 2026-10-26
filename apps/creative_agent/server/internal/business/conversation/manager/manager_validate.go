package manager

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
)

// ValidateStage 校验阶段（第 8 章 §8.4.2 分阶段报错）。
type ValidateStage int

const (
	// StageJSONParse 参数非合法 JSON：置空 + Retry 回显原始参数。
	StageJSONParse ValidateStage = iota
	// StageSchemaValidate JSON 合法但不满足 Schema：保留参数 + 精准报错。
	StageSchemaValidate
)

// ValidateResult 是 generate_response 参数校验结果。
type ValidateResult struct {
	Stage         ValidateStage
	OK            bool
	Message       string       // 给 LLM 看的纠错提示（ActionRetry 注入）
	FriendlyReply string       // 校验通过时的友好回复
	Plan          *PlanOutput  // task_type=new_agents_plan 时解析出的计划
	Card          *HotCardData // task_type=push_card 时解析出的卡（创作功能卡等）
}

// ValidTaskTypes 是 task_type 合法枚举（第 8 章 §8.2.2）。
var ValidTaskTypes = map[string]bool{
	"only_reply": true, "push_card": true, "new_agents_plan": true, "execute_agent": true,
}

// ValidateGenerateResponse 校验 generate_response 工具的参数（约束③）。
//
// 流程（第 8 章 §8.4.2）：RepairJSON 修复 → task_type 枚举校验 →
// task_arguments 解析（string / object 容错）→ new_agents_plan 的 agent 白名单校验。
func ValidateGenerateResponse(arguments string) *ValidateResult {
	// 1. JSON 修复（去 Markdown 包裹等）
	repaired, ok := RepairJSON(arguments)
	if !ok {
		return &ValidateResult{
			Stage:   StageJSONParse,
			OK:      false,
			Message: "参数不是合法的 JSON 结构，请检查。原始参数：" + truncate(arguments, 200),
		}
	}

	// 2. 解析顶层字段（用 map 而非 struct：task_arguments 可能是 string 或 object）
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

	// 3. task_type 枚举校验
	if !ValidTaskTypes[taskType] {
		return &ValidateResult{
			Stage:   StageSchemaValidate,
			OK:      false,
			Message: fmt.Sprintf("task_type 取值不合法：%q，必须是以下之一：only_reply / push_card / new_agents_plan / execute_agent", taskType),
		}
	}

	// 4. task_arguments：string / object 容错
	taskArgsJSON := "{}"
	switch tv := raw["task_arguments"].(type) {
	case string:
		taskArgsJSON = tv
	case map[string]any:
		b, _ := json.Marshal(tv)
		taskArgsJSON = string(b)
	case nil:
		// 缺省 → "{}"
	default:
		return &ValidateResult{
			Stage:   StageSchemaValidate,
			OK:      false,
			Message: "task_arguments 必须是 JSON 对象或对象字符串",
		}
	}

	// 5. task_arguments 必须是合法 JSON 对象
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

	// 6. new_agents_plan → 校验产线 + 校验 LLM 填的 agent 属于产线 → 服务端翻译链
	//    agents_pipeline 是 generate_response 顶层参数（Schema enum 硬约束，§8.4.4）
	if taskType == "new_agents_plan" {
		pipelineName, _ := raw["agents_pipeline"].(string)
		plan, msg := validateAndBuildPlan(friendlyReply, pipelineName, taskArgs)
		if plan == nil {
			return &ValidateResult{Stage: StageSchemaValidate, OK: false, Message: msg}
		}
		return &ValidateResult{Stage: StageSchemaValidate, OK: true, FriendlyReply: friendlyReply, Plan: plan}
	}

	// 6.5 push_card → 解析卡数据（创作功能卡等）。task_arguments 含 card_type +
	//     title_data（[{title,content,eid}]）或 prompt_items（[{title,text,index,eid,imageUrl}]）。
	if taskType == "push_card" {
		card := parsePushCard(taskArgs, friendlyReply)
		return &ValidateResult{Stage: StageSchemaValidate, OK: true, FriendlyReply: friendlyReply, Card: card}
	}

	return &ValidateResult{Stage: StageSchemaValidate, OK: true, FriendlyReply: friendlyReply}
}

// parsePushCard 解析 push_card 的 task_arguments → HotCardData。
// task_arguments 结构（对齐参考）：
//
//	{card_type, label, hotspot_data, title_data:[{title,content,eid}],
//	 prompt_items:[{title,text,index,eid,imageUrl}]}
//
// prompt_items 优先（服务端预定义卡可带 imageUrl）；否则用 title_data 转换。
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

	// prompt_items 优先（可带 imageUrl）
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

	// title_data 转换
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

// creationTypePipeline 把创作功能名映射到产线（AI成片→mix_video / AI笔记图→article）。
// 未识别返回空（前端按普通选项处理）。
func creationTypePipeline(title string) string {
	switch title {
	case "AI成片", "混剪成片", "AI成片混剪":
		return "mix_video"
	case "AI笔记图", "图文笔记", "AI笔记":
		return "article"
	}
	return ""
}

// validateAndBuildPlan 校验 new_agents_plan 参数并组装 Plan。
// 返回 (plan, 纠错提示)：plan 为 nil 时提示非空（精准报错，供 ActionRetry 注入）。
func validateAndBuildPlan(friendlyReply, pipelineName string, taskArgs map[string]any) (*PlanOutput, string) {
	// 1. agents_pipeline：generate_response 顶层参数（Schema enum 硬约束，§8.4.4）。
	//    兼容旧协议：顶层没填时回退读 task_arguments.agents_pipeline（fake 链路/老测试）。
	//    填不存在的键 → 精准报错（enum 是模型层约束，服务端校验仍做最终防线）
	if pipelineName == "" {
		pipelineName, _ = taskArgs["agents_pipeline"].(string)
	}
	if pipelineName == "" {
		return nil, "new_agents_plan 需要 agents_pipeline（generate_response 顶层参数）：从服务端产线模板选键（" + listValidPipelines() + "），再填 agents 数组决定链"
	}
	if _, err := subtask.GetPipelineTemplate(pipelineName); err != nil {
		return nil, pipelineErrorHint + listValidPipelines() + "（你填了 " + pipelineName + "）"
	}

	// 2. 校验 LLM 填的 input agent：必须属于所选产线，防越权装配
	//    （§8.4.4：LLM 决定链怎么装配，但每个 agent 必须是所选产线内的——防止
	//     LLM 在 mix_video 产线里塞 image_painter 之类越权 agent；选题 idea_king
	//     走 idea_king_api 工具，不在此列）
	llmAgents := parseLLMAgents(taskArgs)
	for _, a := range llmAgents {
		if a.AgentID == "" {
			continue // 只有 input 无 agent_id 的项 → 服务端忽略，不报错
		}
		if !pipelineContainsAgent(pipelineName, a.AgentID) {
			return nil, fmt.Sprintf("agent_id %q 不在产线 %s 的 agent 集合内（可选："+listValidPipelines()+"的 chain agent，选题 idea_king 走 idea_king_api 工具不进 plan）", a.AgentID, pipelineName)
		}
	}

	// 3. 服务端翻译：链 = LLM 填的 agents（剔 idea_king、补缺失 input），不展开固定链
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

// parseLLMAgents 从 task_arguments 提取 LLM 填的 agent 列表（agent_id + input）。
// 兼容两种形态：
//   - 新协议：agents 列表（LLM 只填产线内的 agent 及其 input）
//   - 旧协议：plan_steps.agents（兼容 Phase 3 假 LLM / 老测试，校验失败路径同新协议）
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

// pipelineContainsAgent 判断 agent_id 是否在产线的合法 agent 集合内。
// idea_king 除外：它虽是产线链首，但走 idea_king_api 工具，不进 plan（§8.4.3）。
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

// asString 安全取字符串字段（nil / 非 string 一律空串）。
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// RepairJSON 轻量 JSON 修复（第 8 章 §8.4.2）：
//  1. 去 ```json ... ``` Markdown 包裹
//  2. 去首尾空白
//  3. json.Valid 校验
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

// truncate 截断字符串到 n 个字符（报错回显用，避免刷屏）。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
