package handler

import (
	"net/http"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// ClarificationDeps 是点卡确认接口的依赖。
type ClarificationDeps struct {
	Memory *memory.IntentMemory
}

// ClarificationRequest 是 POST /aigc/agent/submit_clarification 的请求体。
//
// 对应真实系统 components.go:37 的 submit_clarification——
// 用户点卡后，把卡片选择写回 intent_memory 并推进 FlowStage。
type ClarificationRequest struct {
	// SessionID 会话 ID（必填）。
	SessionID string `json:"sessionId"`
	// CardType 卡片类型：topic / category / params / script。
	CardType string `json:"cardType"`
	// Value 用户的选择（卡片选项的值）。
	Value string `json:"value"`
	// AgentID 当前 agent（推进后记录，供 GetNextAgent 计算下一个）。
	AgentID string `json:"agentId"`
	// Pipeline 当前产线（如 mix_video）。
	Pipeline string `json:"pipeline"`
	// MsgID 触发本轮产出的 server 消息 ID（脚本卡必填）。
	// 点卡时前端回传，服务端作为 OriginMsgID 回传，供下一轮请求
	// fastpath 追溯前序脚本（对齐真实系统 OriginMsgID 链路）。
	MsgID string `json:"msgId"`
	// SelectedEid 用户点选的热点 eid（热点卡 topic 确认时回传）。
	SelectedEid string `json:"selectedEid"`
	// TopicTitle 用户点选的选题标题（热点卡 topic 确认时回传）。
	TopicTitle string `json:"topicTitle"`
}

// ClarificationResponse 返回推进后的意图记忆快照。
type ClarificationResponse struct {
	Stage    string `json:"stage"`
	Pipeline string `json:"pipeline,omitempty"`
	Agent    string `json:"agent,omitempty"`
	// NextAgent 下一步应执行的 agent（由 GetNextAgent 算出，空表示链结束）。
	NextAgent string `json:"nextAgent,omitempty"`
	// OriginMsgID 回传点卡时的 msgId，前端下一轮请求带回（组织上下文用）。
	OriginMsgID string `json:"originMsgId,omitempty"`
	// SelectedEid 回传用户点选的热点 eid（热点卡 topic 确认后）。
	SelectedEid string `json:"selectedEid,omitempty"`
	// TopicTitle 回传用户点选的选题标题。
	TopicTitle string `json:"topicTitle,omitempty"`
}

// SubmitClarificationHandler 返回点卡确认的 HTTP handler。
//
// 对应真实系统 §5.5 路径 A ③：用户点卡 → POST /submit_clarification
// → updateIntentMemoryOnCardClick 把 memory.FlowStage 推进。
// 推进后的 FlowStage 是下一次请求 CanFastTask 判断的依据。
func SubmitClarificationHandler(deps ClarificationDeps) yukino.Middleware {
	return func(c *yukino.Context, _ func()) {
		var req ClarificationRequest
		if err := c.BindJSON(&req); err != nil {
			response.Fail(c, http.StatusBadRequest, 40000, "请求体格式错误")
			return
		}
		if req.SessionID == "" {
			response.Fail(c, http.StatusBadRequest, 40001, "sessionId 是必填字段")
			return
		}

		nextStage := mapCardTypeToStage(req.CardType)
		if nextStage == "" {
			response.Fail(c, http.StatusBadRequest, 40002, "cardType 不合法（topic/category/params/plan/script）")
			return
		}
		// 脚本卡必带 msgId：fastpath 投递下一个 agent 时要靠它追溯前序脚本
		// （对齐真实系统 OriginMsgID 链路，script 卡确认后必须能组织上下文）。
		if nextStage == memory.FlowStageScriptConfirmed && req.MsgID == "" {
			response.Fail(c, http.StatusBadRequest, 40003, "脚本卡确认必须携带 msgId（用于追溯前序脚本）")
			return
		}

		// 写回 intent_memory 并推进 FlowStage
		if req.Pipeline != "" {
			deps.Memory.SetPipeline(req.SessionID, req.Pipeline)
		}
		if req.AgentID != "" {
			deps.Memory.SetAgent(req.SessionID, req.AgentID)
		}
		// 热点卡（topic）确认：记录用户点选的热点 eid + 选题标题
		if nextStage == memory.FlowStageTopicConfirmed {
			if req.SelectedEid != "" {
				deps.Memory.SetTopicEid(req.SessionID, req.SelectedEid)
			}
			if req.TopicTitle != "" {
				deps.Memory.SetTopicTitle(req.SessionID, req.TopicTitle)
			}
		}
		deps.Memory.SetStage(req.SessionID, nextStage)

		// 计算下一个 agent（脚本确认后进入下一阶段；其他卡片确认不推进 agent）
		nextAgent := ""
		if nextStage == memory.FlowStageScriptConfirmed && req.Pipeline != "" && req.AgentID != "" {
			next, err := subtask.GetNextAgent(subtask.Pipeline(req.Pipeline), subtask.AgentID(req.AgentID))
			if err == nil {
				nextAgent = string(next)
			}
		}

		snap := deps.Memory.Snapshot(req.SessionID)
		response.OK(c, ClarificationResponse{
			Stage:       snap["stage"],
			Pipeline:    snap["pipeline"],
			Agent:       snap["agent"],
			NextAgent:   nextAgent,
			OriginMsgID: req.MsgID,
			SelectedEid: req.SelectedEid,
			TopicTitle:  req.TopicTitle,
		})
	}
}

// mapCardTypeToStage 把卡片类型映射为要推进到的 FlowStage。
//
//	选题卡 topic    → topic_confirmed
//	品类卡 category → category_confirmed
//	参数卡 params   → params_confirmed
//	策划卡 plan     → plan_confirmed
//	脚本卡 script   → script_confirmed（投递下一个 agent 的触发点）
func mapCardTypeToStage(cardType string) memory.FlowStage {
	switch cardType {
	case "topic":
		return memory.FlowStageTopicConfirmed
	case "category":
		return memory.FlowStageCategoryConfirmed
	case "params":
		return memory.FlowStageParamsConfirmed
	case "plan":
		return memory.FlowStagePlanConfirmed
	case "script":
		return memory.FlowStageScriptConfirmed
	}
	return ""
}
