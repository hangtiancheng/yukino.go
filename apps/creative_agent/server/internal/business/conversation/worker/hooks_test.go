package worker_test

import (
	"context"
	"sync"
	"testing"

	appkitfake "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/fake"
	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/worker"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// recordingLLM records the messages received by each LLM call, enabling assertions on prompts/constraints.
// responses are consumed cyclically (the last one repeats once exhausted); turn counts from 1.
type recordingLLM struct {
	mu        sync.Mutex
	turn      int
	messages  [][]react.Message
	responses []*react.LLMResponse
}

func (l *recordingLLM) Chat(_ context.Context, messages []react.Message, _ []react.FunctionDef) (*react.LLMResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.turn++
	l.messages = append(l.messages, append([]react.Message(nil), messages...))
	i := l.turn - 1
	if i >= len(l.responses) {
		i = len(l.responses) - 1
	}
	return l.responses[i], nil
}

// ChatStream implements the react.LLM interface (streaming degradation: forward to Chat + trigger one ending).
func (l *recordingLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	if err != nil {
		return resp, err
	}
	if cb != nil && resp != nil {
		_ = cb(ctx, &react.StreamedChatResponsePayload{
			Choices: []react.StreamedChatChoice{{Delta: react.StreamedChatDelta{Role: "assistant", Content: resp.Content}}},
		}, true)
	}
	return resp, nil
}

// firstMessages returns the initial messages received by that LLM call (the set produced by the engine's prepare call).
func (l *recordingLLM) firstMessages() []react.Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.messages) == 0 {
		return nil
	}
	return l.messages[0]
}

// recordingTools records tool executions and returns a ToolsProvider that can execute any tool.
type recordingTools struct {
	mu       sync.Mutex
	executed []string
}

func (t *recordingTools) Execute(name, args string) (string, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.executed = append(t.executed, name)
	return `{"ok":true}`, true, nil
}

func (t *recordingTools) executedNames() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.executed...)
}

// validPlanResponse returns one valid generate_response new_agents_plan tool call
// (ManagerStrategy constraint ③ passes → single-round Terminal → onOutput produces the plan).
func validPlanResponse() *react.LLMResponse {
	return &react.LLMResponse{
		Content: "我需要拆分任务给子智能体。",
		ToolCalls: []react.ToolCall{{
			ID:   "call_plan",
			Name: "generate_response",
			Args: `{"thought":"t","friendly_reply":"已为你派发子任务，请稍候。","task_type":"new_agents_plan","agents_pipeline":"mix_video","task_arguments":"{\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"生成一张海报\"}]}"}`,
		}},
	}
}

// TestUseManagerStrategy_InjectsManagerPromptAndPlan locks in the Task 3 behavior-regression fix:
// with UseManagerStrategy=true, ApplyReActSetup must inject the manager constraint chain (ManagerStrategy +
// ManagerPrepare); the LLM receives the constraint prompt in the first round, and a valid generate_response produces the plan in one round.
// Without the prompt injection (regression), the LLM would receive appkit's generic assistant prompt and this test would fail.
func TestUseManagerStrategy_InjectsManagerPromptAndPlan(t *testing.T) {
	recLLM := &recordingLLM{responses: []*react.LLMResponse{validPlanResponse()}}
	recTools := &recordingTools{}

	runner := &appkitrunner.SupervisorRunner{}
	runner.SetProviders(recLLM, nil, &appkitfake.FakeHooks{})
	runner.SetPlanTools(recTools)

	hooks := &worker.TaskRunnerHooks{
		// Aligned with the app wiring: the app sets UseManagerStrategy=true when a real manager LLM is configured.
		UseManagerStrategy: true,
	}
	hooks.ApplyReActSetup(runner, func(cp *react.Checkpoint) {})

	var gotPlan *appkitrunner.PlanOutput
	_, err := runner.RunWithOutput(context.Background(), "生成一张海报", "msg-manager",
		func(appkitrunner.SSEFrame) {}, func(p *appkitrunner.PlanOutput) { gotPlan = p })
	testx.NoError(t, err)

	// ① The manager prompt is injected: the first LLM call receives ManagerPrepare's system prompt.
	first := recLLM.firstMessages()
	testx.NotEmpty(t, first, "the LLM should receive at least one initial message")
	testx.Equal(t, "system", first[0].Role)
	testx.Contains(t, first[0].Content, "你是管家智能体",
		"UseManagerStrategy=true must inject the manager constraint-chain prompt (Task 3 regression fix)")
	testx.Contains(t, first[0].Content, "generate_response")
	testx.Contains(t, first[0].Content, "new_agents_plan")
	testx.Equal(t, "生成一张海报", first[len(first)-1].Content)

	// ② The constraint chain produces the plan in one round: a valid generate_response reaches Terminal directly, no tool execution.
	testx.NotNil(t, gotPlan, "the manager should produce new_agents_plan")
	testx.Equal(t, "new_agents_plan", gotPlan.TaskType)
	testx.Equal(t, "mix_video", gotPlan.AgentsPipeline)
}

// TestUseManagerStrategy_ConstraintChainRejectsNonWhitelistedTool locks in that
// SetPlanStrategy(manager.ManagerStrategy) is really injected (constraint ② tool-name whitelist):
// the LLM repeatedly returns dispatch_agent outside the whitelist → ManagerStrategy Retries every time (no execution),
// the retry limit errors out, and recTools stays empty throughout. Without strategy injection (FakePlanStrategy default),
// dispatch_agent would be executed via Continue → recTools non-empty → this test would fail.
func TestUseManagerStrategy_ConstraintChainRejectsNonWhitelistedTool(t *testing.T) {
	recLLM := &recordingLLM{responses: []*react.LLMResponse{{
		Content:   "我要派发子任务。",
		ToolCalls: []react.ToolCall{{ID: "call_dispatch", Name: "dispatch_agent", Args: `{"agent_id":"gold_scriptwriter","input":"生成一张海报"}`}},
	}}}
	recTools := &recordingTools{}

	runner := &appkitrunner.SupervisorRunner{}
	runner.SetProviders(recLLM, nil, &appkitfake.FakeHooks{})
	runner.SetPlanTools(recTools)

	hooks := &worker.TaskRunnerHooks{UseManagerStrategy: true}
	hooks.ApplyReActSetup(runner, func(cp *react.Checkpoint) {})

	_, err := runner.RunWithOutput(context.Background(), "生成一张海报", "msg-reject",
		func(appkitrunner.SSEFrame) {}, func(*appkitrunner.PlanOutput) {})
	testx.Error(t, err, "tools outside the whitelist should be rejected by the constraint chain (retry limit error)")
	testx.Empty(t, recTools.executedNames(),
		"dispatch_agent is not in the manager whitelist; constraint ② should reject it instead of executing it (locking in the ManagerStrategy injection)")
}

// TestNoUseManagerStrategy_Fallback is the inverse control:
// with UseManagerStrategy=false the manager prompt is not injected; the LLM receives the default assistant prompt,
// equivalent to the original consumer wiring without the constraint chain (FakePlanStrategy default).
func TestNoUseManagerStrategy_Fallback(t *testing.T) {
	// FakePlanStrategy's Terminal criterion is text containing new_agents_plan (no constraint-chain tool validation),
	// so plain-text terminal JSON is used here (no tool_calls).
	recLLM := &recordingLLM{responses: []*react.LLMResponse{{
		Content: `{"task_type":"new_agents_plan","friendly_reply":"已为你派发子任务，请稍候。","agents_pipeline":"mix_video","task_arguments":"{\"agents\":[{\"agent_id\":\"gold_scriptwriter\",\"input\":\"生成一张海报\"}]}"}`,
	}}}

	runner := &appkitrunner.SupervisorRunner{}
	runner.SetProviders(recLLM, nil, &appkitfake.FakeHooks{})

	hooks := &worker.TaskRunnerHooks{UseManagerStrategy: false}
	hooks.ApplyReActSetup(runner, func(cp *react.Checkpoint) {})

	_, err := runner.RunWithOutput(context.Background(), "生成一张海报", "msg-fallback",
		func(appkitrunner.SSEFrame) {}, func(*appkitrunner.PlanOutput) {})
	testx.NoError(t, err)

	first := recLLM.firstMessages()
	testx.NotEmpty(t, first)
	testx.NotContains(t, first[0].Content, "你是管家智能体",
		"UseManagerStrategy=false should not inject the manager prompt")
	testx.Contains(t, first[0].Content, "有帮助的 AI 助手", "it should fall back to the default assistant prompt")
}

// TestCustomManagerPrepare_Override verifies that hooks.ManagerPrepare can be overridden by the app:
// when a custom prepare is injected, the LLM's first round should receive the custom prompt instead of the default manager.ManagerPrepare.
func TestCustomManagerPrepare_Override(t *testing.T) {
	recLLM := &recordingLLM{responses: []*react.LLMResponse{validPlanResponse()}}

	runner := &appkitrunner.SupervisorRunner{}
	runner.SetProviders(recLLM, nil, &appkitfake.FakeHooks{})

	hooks := &worker.TaskRunnerHooks{
		UseManagerStrategy: true,
		ManagerPrepare: func(q string) []react.Message {
			return []react.Message{
				{Role: "system", Content: "自定义管家 prompt（测试覆盖）"},
				{Role: "user", Content: q},
			}
		},
	}
	hooks.ApplyReActSetup(runner, func(cp *react.Checkpoint) {})

	_, err := runner.RunWithOutput(context.Background(), "q", "msg-custom",
		func(appkitrunner.SSEFrame) {}, func(*appkitrunner.PlanOutput) {})
	testx.NoError(t, err)

	first := recLLM.firstMessages()
	testx.NotEmpty(t, first)
	testx.Equal(t, "system", first[0].Role)
	testx.Contains(t, first[0].Content, "自定义管家 prompt")
}
