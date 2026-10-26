package worker

import (
	"encoding/json"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"

	"go.uber.org/zap"
)

// ThoughtHooks 是每轮 LLM 调用后落 thought 表的 hooks（OnAfterLLM 挂点）。
//
// 对齐真实系统（dujia）的 OnAfterLLM → InsertThought：每轮把 model_input/model_output
// 含 tool_calls/reasoning 落 cg_agent_thought，供调试与完整对话过程恢复。
//
// 身份字段（userID/sessionID/agentID/msgID）来自闭包捕获的 conversationPayload
// （go-learn 的 AgentContext 无业务身份字段，不侵入引擎）。
type ThoughtHooks struct {
	react.NopHooks

	repo    repository.AgentThoughtRepository
	payload *conversationPayload // 闭包捕获的业务身份
	model   string               // LLM 模型名（未知可空）
	logger  *zap.Logger
}

// TestPayload 是测试用的身份载体（替代 conversationPayload，供外部包测试 ThoughtHooks）。
// 仅测试使用：生产路径用 NewThoughtHooks（闭包捕获 conversationPayload）。
type TestPayload struct {
	UserID    int64
	SessionID string
	AgentID   string
	MsgID     string
}

// NewThoughtHooksForTest 创建 ThoughtHooks（测试用，显式传身份字段）。
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

// NewThoughtHooks 创建 ThoughtHooks。repo 为 nil 时退化为 no-op（不落表）。
func NewThoughtHooks(repo repository.AgentThoughtRepository, payload *conversationPayload, model string, logger *zap.Logger) *ThoughtHooks {
	return &ThoughtHooks{repo: repo, payload: payload, model: model, logger: logger}
}

// OnAfterLLM 每轮 LLM 调用后调用：落一条 thought 记录。
func (h *ThoughtHooks) OnAfterLLM(c *react.AgentContext) {
	if h.repo == nil || h.payload == nil {
		return
	}
	now := time.Now().UnixMilli()

	// model_input：引擎塞的 LLM 调用前 messages 快照（分离 system prompt）
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

	// model_output：本轮 LLM 完整响应
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

	// token 统计（流式 usage 未回填时为空，可后续扩展）
	var tokenCount string
	if resp != nil {
		// 复刻：无 usage 字段，落空 JSON 占位
		tokenCount = "{}"
	}

	// start_time：引擎塞的本轮开始时间戳（毫秒）
	startTime, _ := c.Get(react.LLMStepStartKey).(int64)
	if startTime == 0 {
		startTime = now
	}

	thought := &model.CGAgentThought{
		AgentID:      h.payload.AgentID,
		AgentType:    h.payload.AgentID, // 复刻：用 agentID 兜底（管家/子智能体）
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
