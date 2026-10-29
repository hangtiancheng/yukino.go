package react_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// ---------- Fake implementations for testing ----------

// fakeLLM with lookup table: round 1 returns tool_call, round 2 returns only_reply.
type fakeLLM struct{ turn int }

func (l *fakeLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	_ = ctx
	l.turn++
	hasToolResult := false
	for _, m := range messages {
		if m.Role == "tool" {
			hasToolResult = true
			break
		}
	}
	if !hasToolResult {
		return &react.LLMResponse{
			Content:   "I need to search",
			ToolCalls: []react.ToolCall{{ID: "tc1", Name: "search", Args: `{"q":"weather"}`}},
		}, nil
	}
	return &react.LLMResponse{Content: `{"task_type":"only_reply","friendly_reply":"It is sunny"}`}, nil
}

func (l *fakeLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamDegraded(ctx, resp, err, cb)
}

type fakeTools struct{}

func (fakeTools) Execute(name, args string) (string, bool, error) {
	if name == "search" {
		return `{"result":"sunny, 25C"}`, true, nil
	}
	return "", false, fmt.Errorf("unknown tool: %s", name)
}

type demoStrategy struct{}

func (demoStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return []react.FunctionDef{{Name: "search"}}
}

func (demoStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse
	if len(resp.ToolCalls) > 0 {
		return &react.Decision{Action: react.ActionContinue}
	}
	if strings.Contains(resp.Content, "only_reply") {
		return &react.Decision{
			Action:   react.ActionTerminal,
			Metadata: map[string]any{"friendly_reply": extractReply(resp.Content)},
		}
	}
	return &react.Decision{
		Action:   react.ActionRetry,
		Metadata: map[string]any{"retry_message": "Please output only_reply format"},
	}
}

func extractReply(content string) string {
	const key = `"friendly_reply":"`
	if i := strings.Index(content, key); i >= 0 {
		rest := content[i+len(key):]
		if j := strings.Index(rest, `"`); j >= 0 {
			return rest[:j]
		}
	}
	return content
}

// ---------- Tests ----------

func TestSupervisor_HappyPath(t *testing.T) {
	sup := &react.Supervisor{}
	sup.SetMaxStep(10)
	sup.SetMaxRelay(3)
	sup.SetMaxRetry(2)
	sup.SetLLM(&fakeLLM{})
	sup.SetTools(fakeTools{})
	sup.SetStrategy(demoStrategy{})
	sup.SetHooks(react.NopHooks{})
	sup.SetPrepare(func(query string) []react.Message {
		return []react.Message{
			{Role: "system", Content: "You are a helpful assistant"},
			{Role: "user", Content: query},
		}
	})

	result, err := sup.Run(react.NewAgentContext("What is the weather today?"))
	testx.NoError(t, err)
	testx.Equal(t, "It is sunny", result.Content)
	testx.Equal(t, 2, result.StepCount, "should take 2 steps: tool_call + terminal")
}

// ---------- Scripted implementations for all-branches test ----------

// scriptedLLM returns pre-programmed responses to exercise specific branches.
type scriptedLLM struct {
	turn    int
	replies []*react.LLMResponse
}

func (l *scriptedLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	_ = ctx
	r := l.replies[l.turn]
	if l.turn < len(l.replies)-1 {
		l.turn++
	}
	return r, nil
}

func (l *scriptedLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamDegraded(ctx, resp, err, cb)
}

type flakyTools struct{ calls int }

func (t *flakyTools) Execute(name, args string) (string, bool, error) {
	if name == "get_weather" {
		t.calls++
		if t.calls == 1 {
			return "", false, fmt.Errorf("downstream timeout")
		}
		return `{"weather":"clear"}`, true, nil
	}
	return "", false, fmt.Errorf("unknown tool: %s", name)
}

type scriptStrategy struct{}

func (scriptStrategy) GetTools(c *react.AgentContext) []react.FunctionDef {
	return []react.FunctionDef{{Name: "get_weather"}}
}

func (scriptStrategy) Decide(c *react.AgentContext) *react.Decision {
	resp := c.Runtime.LastResponse
	switch {
	case strings.Contains(resp.Content, "BAD_FORMAT"):
		return &react.Decision{
			Action:   react.ActionRetry,
			Metadata: map[string]any{"retry_message": "Format is wrong, try again"},
		}
	case strings.Contains(resp.Content, "RELAY"):
		return &react.Decision{
			Action:   react.ActionRelay,
			Metadata: map[string]any{"relay_message": "Now summarize it"},
		}
	case len(resp.ToolCalls) > 0:
		return &react.Decision{Action: react.ActionContinue}
	case strings.Contains(resp.Content, "only_reply"):
		return &react.Decision{
			Action:   react.ActionTerminal,
			Metadata: map[string]any{"friendly_reply": extractReply(resp.Content)},
		}
	}
	return &react.Decision{Action: react.ActionTerminal, Metadata: map[string]any{"friendly_reply": resp.Content}}
}

func TestSupervisor_AllBranches(t *testing.T) {
	// Script:
	//   round 1: BAD_FORMAT → Retry (step doesn't increment)
	//   round 2: tool call   → Continue (tool fails, error as observation)
	//   round 3: tool call   → Continue (tool succeeds)
	//   round 4: RELAY       → Relay (clear messages, goto preparePrompt)
	//   round 5: only_reply  → Terminal
	scripted := &scriptedLLM{replies: []*react.LLMResponse{
		{Content: "BAD_FORMAT output"},
		{Content: "Check weather", ToolCalls: []react.ToolCall{{ID: "a", Name: "get_weather"}}},
		{Content: "Try again", ToolCalls: []react.ToolCall{{ID: "b", Name: "get_weather"}}},
		{Content: "RELAY need server followup"},
		{Content: `{"task_type":"only_reply","friendly_reply":"All done!"}`},
	}}
	sup := &react.Supervisor{}
	sup.SetMaxStep(10)
	sup.SetMaxRelay(3)
	sup.SetMaxRetry(2)
	sup.SetLLM(scripted)
	sup.SetTools(&flakyTools{})
	sup.SetStrategy(scriptStrategy{})
	sup.SetHooks(react.NopHooks{})
	sup.SetPrepare(func(query string) []react.Message {
		return []react.Message{{Role: "user", Content: query}}
	})

	result, err := sup.Run(react.NewAgentContext("original query"))
	testx.NoError(t, err)
	testx.Equal(t, "All done!", result.Content)
	testx.Equal(t, 1, result.StepCount, "post-relay should count steps from 1")
}

// ---------- HIL resume tests ----------

// resumeLLM returns a tool_call on the first call and terminal text afterwards.
type resumeLLM struct{ turn int }

func (l *resumeLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	_ = ctx
	l.turn++
	hasTool := false
	for _, m := range messages {
		if m.Role == "tool" {
			hasTool = true
			break
		}
	}
	if !hasTool {
		return &react.LLMResponse{ToolCalls: []react.ToolCall{{ID: "c1", Name: "probe"}}}, nil
	}
	return &react.LLMResponse{Content: "resume-terminal"}, nil
}

func (l *resumeLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
	resp, err := l.Chat(ctx, messages, tools)
	return streamDegraded(ctx, resp, err, cb)
}

// streamDegraded is the test stub's streaming degradation: synchronous result + one isEndingChunk=true callback.
func streamDegraded(ctx context.Context, resp *react.LLMResponse, err error, cb react.StreamCallbackFunc) (*react.LLMResponse, error) {
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

type resumeTools struct{}

func (resumeTools) Execute(name, args string) (string, bool, error) {
	return "probe-ok", true, nil
}

// resumeStrategy produces Interrupt at step==0 and Terminal otherwise.
type resumeStrategy struct{}

func (resumeStrategy) GetTools(*react.AgentContext) []react.FunctionDef { return nil }

func (resumeStrategy) Decide(c *react.AgentContext) *react.Decision {
	if c.Runtime.Step == 0 && !c.Runtime.Interrupted {
		return &react.Decision{Action: react.ActionInterrupt, Metadata: map[string]any{"friendly_reply": "卡住了，等用户操作"}}
	}
	return &react.Decision{Action: react.ActionTerminal, Metadata: map[string]any{"friendly_reply": "resume-terminal"}}
}

// TestResumeFromCheckpoint verifies resuming from the breakpoint after an HIL interrupt:
// the first Run interrupts → checkpoint saved; the second pre-loads the checkpoint → continues from the original step instead of restarting.
func TestResumeFromCheckpoint(t *testing.T) {
	llm := &resumeLLM{}
	sup := &react.Supervisor{}
	sup.SetMaxStep(10)
	sup.SetMaxRelay(3)
	sup.SetMaxRetry(2)
	sup.SetLLM(llm)
	sup.SetTools(resumeTools{})
	sup.SetStrategy(resumeStrategy{})
	sup.SetHooks(react.NopHooks{})
	sup.SetPrepare(func(q string) []react.Message {
		return []react.Message{{Role: "user", Content: q}}
	})

	// First Run: after one tool_call step, the strategy decides Interrupt
	ctx := react.NewAgentContext("resume-test")
	_, err := sup.Run(ctx)
	testx.NoError(t, err)

	// The checkpoint was saved at the interrupt (full structure: messages + step)
	cpRaw := ctx.Get(react.CheckpointKey)
	testx.NotNil(t, cpRaw, "a checkpoint should be saved on Interrupt")
	cp, ok := cpRaw.(*react.Checkpoint)
	testx.True(t, ok, "the checkpoint should be a *react.Checkpoint")
	testx.NotEmpty(t, cp.Messages)
	testx.GreaterOrEqual(t, cp.Step, 1)

	// Second Run: pre-load the checkpoint and continue from the breakpoint
	resumedCtx := react.NewAgentContext("resume-test")
	resumedCtx.Runtime.Messages = cp.Messages
	resumedCtx.Runtime.Step = cp.Step
	resumedCtx.Runtime.Interrupted = true
	resumedCtx.Set("resumed", true)
	result, err := sup.Run(resumedCtx)
	testx.NoError(t, err)
	testx.Equal(t, "resume-terminal", result.Content)
	testx.GreaterOrEqual(t, result.StepCount, 2, "resume should continue from the breakpoint step instead of restarting")
}
