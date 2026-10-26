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

// TestThoughtHooks_OnAfterLLM 验证 OnAfterLLM 每轮落一条 thought 记录：
// model_input（分离 system prompt）/ model_output / tool_calls / 身份字段。
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

	// 构造 AgentContext：引擎会在 OnAfterLLM 前塞 LLMInputMessagesKey + LLMStepStartKey
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

	// 查 thought 记录
	var count int64
	testx.NoError(t, db.Model(&model.CGAgentThought{}).Count(&count).Error)
	testx.Equal(t, int64(1), count, "每轮应落一条 thought")

	var rows []model.CGAgentThought
	testx.NoError(t, db.Find(&rows).Error)
	row := rows[0]

	testx.Equal(t, int64(42), row.UserID)
	testx.Equal(t, "sess-1", row.SessionID)
	testx.Equal(t, "manager", row.AgentID)
	testx.Equal(t, "msg-1", row.MsgID)
	testx.Equal(t, "deepseek-chat", row.Model)
	testx.Equal(t, model.ThoughtStatusCompleted, row.Status)

	// model_input 应分离 system prompt
	testx.NotNil(t, row.ModelInput)
	var inputMsgsOut []react.Message
	testx.NoError(t, json.Unmarshal([]byte(*row.ModelInput), &inputMsgsOut))
	testx.Len(t, inputMsgsOut, 1, "model_input 应只含非 system 消息")
	testx.Equal(t, "user", inputMsgsOut[0].Role)

	// model_output 含 tool_calls
	testx.NotNil(t, row.ModelOutput)
	var outputMsg react.Message
	testx.NoError(t, json.Unmarshal([]byte(*row.ModelOutput), &outputMsg))
	testx.Equal(t, "assistant", outputMsg.Role)
	testx.Len(t, outputMsg.ToolCalls, 1)
	testx.Equal(t, "generate_response", outputMsg.ToolCalls[0].Name)

	// start_time 用引擎塞的轮级时间戳
	testx.Equal(t, int64(1000), row.StartTime)
}

// TestThoughtHooks_NilRepo_NoOp 验证 repo 为 nil 时不落表（no-op）。
func TestThoughtHooks_NilRepo_NoOp(t *testing.T) {
	hooks := worker.NewThoughtHooksForTest(nil, &worker.TestPayload{}, "", nil)
	c := react.NewAgentContext("hi")
	c.Set(react.LLMInputMessagesKey, []react.Message{{Role: "user", Content: "hi"}})
	c.Runtime.LastResponse = &react.LLMResponse{Content: "ok"}

	// 不 panic 即可
	hooks.OnAfterLLM(c)
}
