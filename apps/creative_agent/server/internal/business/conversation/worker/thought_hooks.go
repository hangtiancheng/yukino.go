package worker

import (
	"encoding/json"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"

	"go.uber.org/zap"
)

// ThoughtHooks persists a thought-table row after every LLM round (the OnAfterLLM hook point).
//
// Aligned with the reference system's (dujia) OnAfterLLM → InsertThought: each round writes model_input/model_output,
// including tool_calls/reasoning, into cg_agent_thought for debugging and full conversation-process recovery.
//
// Identity fields (userID/sessionID/agentID/msgID) come from the closure-captured conversationPayload
// (go-learn's AgentContext has no business identity fields; the engine is not intruded upon).
type ThoughtHooks struct {
	react.NopHooks

	repo    repository.AgentThoughtRepository
	payload *conversationPayload // closure-captured business identity
	model   string               // LLM model name (may be empty when unknown)
	logger  *zap.Logger
}

// TestPayload is the identity carrier for tests (replacing conversationPayload so external packages can test ThoughtHooks).
// Test-only: the production path uses NewThoughtHooks (closure-capturing conversationPayload).
type TestPayload struct {
	UserID    int64
	SessionID string
	AgentID   string
	MsgID     string
}

// NewThoughtHooksForTest creates ThoughtHooks (for tests, with identity fields passed explicitly).
func NewThoughtHooksForTest(repo repository.AgentThoughtRepository, p *TestPayload, model string, logger *zap.Logger) *ThoughtHooks {
	if p == nil {
		p = &TestPayload{}
	}
	cp := &conversationPayload{
		UserID:    p.UserID,
		SessionID: p.SessionID,
		AgentID:   p.AgentID,
		MsgID:     p.MsgID,
	}
	return NewThoughtHooks(repo, cp, model, logger)
}

// NewThoughtHooks creates ThoughtHooks. When repo is nil it degrades to a no-op (no table writes).
func NewThoughtHooks(repo repository.AgentThoughtRepository, payload *conversationPayload, model string, logger *zap.Logger) *ThoughtHooks {
	return &ThoughtHooks{repo: repo, payload: payload, model: model, logger: logger}
}

// OnAfterLLM is called after every LLM round: it persists one thought record.
func (h *ThoughtHooks) OnAfterLLM(c *react.AgentContext) {
	if h.repo == nil || h.payload == nil {
		return
	}
	now := time.Now().UnixMilli()

	// model_input: the pre-call messages snapshot stored by the engine (system prompt separated)
	var systemPrompt *string
	var inputMsgs []react.Message
	if raw, ok := c.Get(react.LLMInputMessagesKey).([]react.Message); ok {
		for _, m := range raw {
			if m.Role == "system" {
				sp := m.Content
				systemPrompt = &sp
			} else {
				inputMsgs = append(inputMsgs, m)
			}
		}
	}
	var modelInput *string
	if b, err := json.Marshal(inputMsgs); err == nil {
		s := string(b)
		modelInput = &s
	}

	// model_output: this round's complete LLM response
	resp := c.Runtime.LastResponse
	var modelOutput *string
	if resp != nil {
		om := react.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls}
		if b, err := json.Marshal(om); err == nil {
			s := string(b)
			modelOutput = &s
		}
	}

	// tool_calls JSON
	var toolCalls string
	if resp != nil && len(resp.ToolCalls) > 0 {
		if b, err := json.Marshal(resp.ToolCalls); err == nil {
			toolCalls = string(b)
		}
	}

	// Token statistics (empty when streaming usage is not backfilled; extensible later)
	var tokenCount string
	if resp != nil {
		// Replica: no usage field; persist an empty JSON placeholder
		tokenCount = "{}"
	}

	// start_time: this round's start timestamp stored by the engine (milliseconds)
	startTime, _ := c.Get(react.LLMStepStartKey).(int64)
	if startTime == 0 {
		startTime = now
	}

	thought := &model.CGAgentThought{
		AgentID:      h.payload.AgentID,
		AgentType:    h.payload.AgentID, // replica: fall back to agentID (manager/sub-agent)
		UserID:       h.payload.UserID,
		Model:        h.model,
		SessionID:    h.payload.SessionID,
		MsgID:        h.payload.MsgID,
		Pd:           "",
		SystemPrompt: systemPrompt,
		ModelInput:   modelInput,
		ModelOutput:  modelOutput,
		StartTime:    startTime,
		EndTime:      now,
		ElapsedMs:    now - startTime,
		Status:       model.ThoughtStatusCompleted,
		ToolCalls:    toolCalls,
		TokenCount:   tokenCount,
		CreateTime:   now,
		UpdateTime:   now,
	}
	if _, err := h.repo.Insert(c.Ctx, thought); err != nil {
		if h.logger != nil {
			h.logger.Error("thought insert failed",
				zap.Error(err), zap.String("msgID", h.payload.MsgID))
		}
	}
}
