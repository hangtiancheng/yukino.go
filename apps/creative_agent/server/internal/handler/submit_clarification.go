package handler

import (
	"net/http"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// ClarificationDeps holds the dependencies of the card-confirmation endpoint.
type ClarificationDeps struct {
	Memory *memory.IntentMemory
}

// ClarificationRequest is the request body of POST /aigc/agent/submit_clarification.
//
// Mirrors submit_clarification in the reference system's components.go:37 —
// after the user clicks a card, the selection is written back to intent_memory and FlowStage advances.
type ClarificationRequest struct {
	// SessionID is the session ID (required).
	SessionID string `json:"sessionId"`
	// CardType is the card type: topic / category / params / script.
	CardType string `json:"cardType"`
	// Value is the user's selection (the card option value).
	Value string `json:"value"`
	// AgentID is the current agent (recorded after advancing, used by GetNextAgent to compute the next one).
	AgentID string `json:"agentId"`
	// Pipeline is the current pipeline (e.g. mix_video).
	Pipeline string `json:"pipeline"`
	// MsgID is the server message ID that triggered this round's output (required for script cards).
	// The frontend sends it when clicking the card; the server echoes it back as OriginMsgID so the next request's
	// fastpath can trace the preceding script (aligned with the reference system's OriginMsgID chain).
	MsgID string `json:"msgId"`
	// SelectedEid is the hot-topic eid the user clicked (sent on hot-topic card confirmation).
	SelectedEid string `json:"selectedEid"`
	// TopicTitle is the topic title the user clicked (sent on hot-topic card confirmation).
	TopicTitle string `json:"topicTitle"`
}

// ClarificationResponse returns the intent-memory snapshot after advancing.
type ClarificationResponse struct {
	Stage    string `json:"stage"`
	Pipeline string `json:"pipeline,omitempty"`
	Agent    string `json:"agent,omitempty"`
	// NextAgent is the agent to execute next (computed by GetNextAgent; empty means the chain ends).
	NextAgent string `json:"nextAgent,omitempty"`
	// OriginMsgID echoes the msgId from the card click; the frontend sends it back on the next request (for context assembly).
	OriginMsgID string `json:"originMsgId,omitempty"`
	// SelectedEid echoes the hot-topic eid the user clicked (after hot-topic card confirmation).
	SelectedEid string `json:"selectedEid,omitempty"`
	// TopicTitle echoes the topic title the user clicked.
	TopicTitle string `json:"topicTitle,omitempty"`
}

// SubmitClarificationHandler returns the HTTP handler for card confirmation.
//
// Mirrors §5.5 path A ③ of the reference system: user clicks a card → POST /submit_clarification
// → updateIntentMemoryOnCardClick advances memory.FlowStage.
// The advanced FlowStage is the basis for the next request's CanFastTask decision.
func SubmitClarificationHandler(deps ClarificationDeps) yukino.Middleware {
	return func(c *yukino.Context, _ func()) {
		var req ClarificationRequest
		if err := c.BindJSON(&req); err != nil {
			response.Fail(c, http.StatusBadRequest, 40000, "malformed request body")
			return
		}
		if req.SessionID == "" {
			response.Fail(c, http.StatusBadRequest, 40001, "sessionId is required")
			return
		}

		nextStage := mapCardTypeToStage(req.CardType)
		if nextStage == "" {
			response.Fail(c, http.StatusBadRequest, 40002, "invalid cardType (topic/category/params/plan/script)")
			return
		}
		// Script cards must carry msgId: fastpath needs it to trace the preceding script when dispatching the next agent
		// (aligned with the reference system's OriginMsgID chain; context must be assemblable after script-card confirmation).
		if nextStage == memory.FlowStageScriptConfirmed && req.MsgID == "" {
			response.Fail(c, http.StatusBadRequest, 40003, "script card confirmation must carry msgId (used to trace the preceding script)")
			return
		}

		// Write back to intent_memory and advance FlowStage
		if req.Pipeline != "" {
			deps.Memory.SetPipeline(req.SessionID, req.Pipeline)
		}
		if req.AgentID != "" {
			deps.Memory.SetAgent(req.SessionID, req.AgentID)
		}
		// Hot-topic card confirmation: record the clicked hot-topic eid + topic title
		if nextStage == memory.FlowStageTopicConfirmed {
			if req.SelectedEid != "" {
				deps.Memory.SetTopicEid(req.SessionID, req.SelectedEid)
			}
			if req.TopicTitle != "" {
				deps.Memory.SetTopicTitle(req.SessionID, req.TopicTitle)
			}
		}
		deps.Memory.SetStage(req.SessionID, nextStage)

		// Compute the next agent (script confirmation enters the next stage; other card confirmations do not advance the agent)
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

// mapCardTypeToStage maps a card type to the FlowStage to advance to.
//
//	topic card    → topic_confirmed
//	category card → category_confirmed
//	params card   → params_confirmed
//	plan card     → plan_confirmed
//	script card   → script_confirmed (the trigger for dispatching the next agent)
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
