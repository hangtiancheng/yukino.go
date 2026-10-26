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

// recordingLLM 记录每次 LLM 调用收到的 messages，便于断言 prompt/约束是否生效。
// responses 循环取用（取尽后重复最后一个），turn 从 1 开始计数。
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

// ChatStream 实现 react.LLM 接口（流式降级：转发 Chat + 触发一次 ending）。
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

// firstMessages 返回该次 LLM 调用收到的初始 messages（即引擎调 prepare 产出的那组）。
func (l *recordingLLM) firstMessages() []react.Message {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.messages) == 0 {
		return nil
	}
	return l.messages[0]
}

// recordingTools 记录工具执行，返回可执行任意工具的 ToolsProvider。
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

// validPlanResponse 返回一次合法的 generate_response new_agents_plan 工具调用
// （ManagerStrategy 约束③ 校验通过 → 单轮 Terminal → onOutput 产出计划）。
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

// TestUseManagerStrategy_InjectsManagerPromptAndPlan 锁定 Task 3 行为回归的修复：
// UseManagerStrategy=true 时 ApplyReActSetup 必须注入管家约束链（ManagerStrategy +
// ManagerPrepare），LLM 首轮就收到约束 prompt，合法 generate_response 单轮产出计划。
// 若无 prompt 注入（回归），LLM 收到的是 appkit 默认通用 assistant prompt，本测试失败。
func TestUseManagerStrategy_InjectsManagerPromptAndPlan(t *testing.T) {
	recLLM := &recordingLLM{responses: []*react.LLMResponse{validPlanResponse()}}
	recTools := &recordingTools{}

	runner := &appkitrunner.SupervisorRunner{}
	runner.SetProviders(recLLM, nil, &appkitfake.FakeHooks{})
	runner.SetPlanTools(recTools)

	hooks := &worker.TaskRunnerHooks{
		// 对齐 app 装配：配置了真实管家 LLM 时 app 置 UseManagerStrategy=true。
		UseManagerStrategy: true,
	}
	hooks.ApplyReActSetup(runner, func(cp *react.Checkpoint) {})

	var gotPlan *appkitrunner.PlanOutput
	_, err := runner.RunWithOutput(context.Background(), "生成一张海报", "msg-manager",
		func(appkitrunner.SSEFrame) {}, func(p *appkitrunner.PlanOutput) { gotPlan = p })
	testx.NoError(t, err)

	// ① 管家 prompt 已注入：首轮 LLM 调用收到 ManagerPrepare 的 system prompt。
	first := recLLM.firstMessages()
	testx.NotEmpty(t, first, "LLM 应收到至少一条初始 message")
	testx.Equal(t, "system", first[0].Role)
	testx.Contains(t, first[0].Content, "你是管家智能体",
		"UseManagerStrategy=true 必须注入管家约束链 prompt（Task 3 回归修复）")
	testx.Contains(t, first[0].Content, "generate_response")
	testx.Contains(t, first[0].Content, "new_agents_plan")
	testx.Equal(t, "生成一张海报", first[len(first)-1].Content)

	// ② 约束链单轮产出计划：合法 generate_response 直达 Terminal，无需执行工具。
	testx.NotNil(t, gotPlan, "管家应产出 new_agents_plan")
	testx.Equal(t, "new_agents_plan", gotPlan.TaskType)
	testx.Equal(t, "mix_video", gotPlan.AgentsPipeline)
}

// TestUseManagerStrategy_ConstraintChainRejectsNonWhitelistedTool 锁定
// SetPlanStrategy(manager.ManagerStrategy) 确实注入（约束② 工具名白名单）：
// LLM 反复返回白名单外的 dispatch_agent → ManagerStrategy 每次 Retry（不执行），
// 重试超限报错，recTools 全程为空。若无策略注入（FakePlanStrategy 默认），
// dispatch_agent 会被 Continue 执行 → recTools 非空 → 本测试失败。
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
	testx.Error(t, err, "白名单外工具应被约束链拒绝（重试超限报错）")
	testx.Empty(t, recTools.executedNames(),
		"dispatch_agent 不在管家白名单，约束② 应拒绝而非执行（锁定 ManagerStrategy 注入）")
}

// TestNoUseManagerStrategy_Fallback 反向对照：
// UseManagerStrategy=false 时不注入管家 prompt，LLM 收到默认 assistant prompt，
// 行为等价原 consumer 无约束链接线（FakePlanStrategy 默认）。
func TestNoUseManagerStrategy_Fallback(t *testing.T) {
	// FakePlanStrategy 的 Terminal 判据是文本含 new_agents_plan（不走约束链工具校验），
	// 故这里用纯文本终态 JSON（无 tool_calls）。
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
		"UseManagerStrategy=false 不应注入管家 prompt")
	testx.Contains(t, first[0].Content, "有帮助的 AI 助手", "应回退默认 assistant prompt")
}

// TestCustomManagerPrepare_Override 覆盖 hooks.ManagerPrepare 可被 app 覆盖：
// 注入自定义 prepare 时，LLM 首轮应收到自定义 prompt 而非默认 manager.ManagerPrepare。
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
