package worker_test

import (
	"encoding/json"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/worker"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestThoughtHooks_OnAfterLLM verifies that OnAfterLLM persists one thought record per round:
// model_input (system prompt separated) / model_output / tool_calls / identity fields.
func TestThoughtHooks_OnAfterLLM(t *testing.T) {
	db := newTestDB(t)
	testx.NoError(t, db.AutoMigrate(&model.CGAgentThought{}))
	repo := repository.NewAgentThoughtRepository(db)

	payload := &worker.TestPayload{
		UserID:    42,
		SessionID: "sess-1",
		AgentID:   "manager",
		MsgID:     "msg-1",
	}
	hooks := worker.NewThoughtHooksForTest(repo, payload, "deepseek-chat", nil)

	// Build the AgentContext: the engine stores LLMInputMessagesKey + LLMStepStartKey before OnAfterLLM
	c := react.NewAgentContext("生成海报")
	inputMsgs := []react.Message{
		{Role: "system", Content: "你是管家"},
		{Role: "user", Content: "生成海报"},
	}
	c.Set(react.LLMInputMessagesKey, inputMsgs)
	c.Set(react.LLMStepStartKey, int64(1000))
	c.Runtime.LastResponse = &react.LLMResponse{
		Content: "已派发",
		ToolCalls: []react.ToolCall{{
			ID: "c1", Name: "generate_response", Args: `{"thought":"t"}`,
		}},
	}

	hooks.OnAfterLLM(c)

	// Query the thought record
	var count int64
	testx.NoError(t, db.Model(&model.CGAgentThought{}).Count(&count).Error)
	testx.Equal(t, int64(1), count, "one thought should be persisted per round")

	var rows []model.CGAgentThought
	testx.NoError(t, db.Find(&rows).Error)
	row := rows[0]

	testx.Equal(t, int64(42), row.UserID)
	testx.Equal(t, "sess-1", row.SessionID)
	testx.Equal(t, "manager", row.AgentID)
	testx.Equal(t, "msg-1", row.MsgID)
	testx.Equal(t, "deepseek-chat", row.Model)
	testx.Equal(t, model.ThoughtStatusCompleted, row.Status)

	// model_input should separate the system prompt
	testx.NotNil(t, row.ModelInput)
	var inputMsgsOut []react.Message
	testx.NoError(t, json.Unmarshal([]byte(*row.ModelInput), &inputMsgsOut))
	testx.Len(t, inputMsgsOut, 1, "model_input should contain only non-system messages")
	testx.Equal(t, "user", inputMsgsOut[0].Role)

	// model_output contains tool_calls
	testx.NotNil(t, row.ModelOutput)
	var outputMsg react.Message
	testx.NoError(t, json.Unmarshal([]byte(*row.ModelOutput), &outputMsg))
	testx.Equal(t, "assistant", outputMsg.Role)
	testx.Len(t, outputMsg.ToolCalls, 1)
	testx.Equal(t, "generate_response", outputMsg.ToolCalls[0].Name)

	// start_time uses the round-level timestamp stored by the engine
	testx.Equal(t, int64(1000), row.StartTime)
}

// TestThoughtHooks_NilRepo_NoOp verifies that no rows are written when repo is nil (no-op).
func TestThoughtHooks_NilRepo_NoOp(t *testing.T) {
	hooks := worker.NewThoughtHooksForTest(nil, &worker.TestPayload{}, "", nil)
	c := react.NewAgentContext("hi")
	c.Set(react.LLMInputMessagesKey, []react.Message{{Role: "user", Content: "hi"}})
	c.Runtime.LastResponse = &react.LLMResponse{Content: "ok"}

	// Not panicking is enough
	hooks.OnAfterLLM(c)
}
