package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino-code/apps/yukino/conversation"
	"github.com/hangtiancheng/yukino-code/apps/yukino/llm"
	"github.com/hangtiancheng/yukino-code/apps/yukino/tools"
)

type provider struct {
	calls  int
	stream func(context.Context, *conversation.Manager, []map[string]any) ([]llm.StreamEvent, error)
}

func (p *provider) SetSystemPrompt(string) {}
func (p *provider) Stream(ctx context.Context, conv *conversation.Manager, schemas []map[string]any) (<-chan llm.StreamEvent, <-chan error) {
	p.calls++
	events, err := p.stream(ctx, conv, schemas)
	ch := make(chan llm.StreamEvent, len(events))
	errs := make(chan error, 1)
	for _, event := range events {
		ch <- event
	}
	if err != nil {
		errs <- err
	}
	close(ch)
	close(errs)
	return ch, errs
}

type echoTool struct{ called bool }

func (t *echoTool) Name() string                 { return "echo" }
func (t *echoTool) Description() string          { return "Echo text" }
func (t *echoTool) Category() tools.ToolCategory { return tools.CategoryRead }
func (t *echoTool) Schema() map[string]any {
	return map[string]any{"name": "echo", "description": "Echo text", "input_schema": map[string]any{"type": "object"}}
}
func (t *echoTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	t.called = true
	return tools.ToolResult{Output: "tool evidence"}
}
func TestRunExecutesToolsAndReturnsFinalAnswer(t *testing.T) {
	tool := &echoTool{}
	registry := tools.NewRegistry()
	registry.Register(tool)
	defer registry.Dispose()
	p := &provider{}
	p.stream = func(_ context.Context, conv *conversation.Manager, schemas []map[string]any) ([]llm.StreamEvent, error) {
		if len(schemas) != 1 {
			t.Errorf("schemas: %v", schemas)
		}
		if p.calls == 1 {
			return []llm.StreamEvent{llm.TextDelta{Text: "Checking."}, llm.ToolCallComplete{ToolID: "call1", ToolName: "echo", Arguments: map[string]any{}}, llm.StreamEnd{StopReason: "tool_use"}}, nil
		}
		messages := conv.GetMessages()
		last := messages[len(messages)-1]
		if len(last.ToolResults) != 1 || last.ToolResults[0].Content != "tool evidence" {
			t.Errorf("missing tool result: %+v", last)
		}
		return []llm.StreamEvent{llm.TextDelta{Text: "Final "}, llm.TextDelta{Text: "answer"}, llm.StreamEnd{StopReason: "end_turn"}}, nil
	}
	conv := conversation.NewManager()
	conv.AddUserMessage("question")
	var streamed strings.Builder
	answer, err := Run(context.Background(), p, "openai-compat", registry, conv, 3, func(s string) { streamed.WriteString(s) })
	if err != nil || answer != "Final answer" || !tool.called || p.calls != 2 || streamed.String() != "Checking.Final answer" {
		t.Fatalf("answer=%q err=%v calls=%d streamed=%q", answer, err, p.calls, streamed.String())
	}
}
func TestRunRejectsIncompleteStreams(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []llm.StreamEvent
		err    error
	}{
		{name: "missing completion", events: []llm.StreamEvent{llm.TextDelta{Text: "partial"}}},
		{name: "provider error", err: errors.New("bad request")},
		{name: "token limit", events: []llm.StreamEvent{llm.TextDelta{Text: "partial"}, llm.StreamEnd{StopReason: "max_tokens"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := tools.NewRegistry()
			defer registry.Dispose()
			p := &provider{stream: func(context.Context, *conversation.Manager, []map[string]any) ([]llm.StreamEvent, error) {
				return tc.events, tc.err
			}}
			conv := conversation.NewManager()
			conv.AddUserMessage("question")
			answer, err := Run(context.Background(), p, "openai-compat", registry, conv, 1, nil)
			if err == nil || answer != "" {
				t.Fatalf("accepted incomplete output: %q %v", answer, err)
			}
		})
	}
}
func TestRunDrainsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	drained := make(chan struct{})
	p := &cancelProvider{drained: drained}
	registry := tools.NewRegistry()
	defer registry.Dispose()
	conv := conversation.NewManager()
	conv.AddUserMessage("question")
	_, err := Run(ctx, p, "openai-compat", registry, conv, 2, func(string) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("provider not drained")
	}
}

type cancelProvider struct{ drained chan struct{} }

func (*cancelProvider) SetSystemPrompt(string) {}
func (p *cancelProvider) Stream(ctx context.Context, _ *conversation.Manager, _ []map[string]any) (<-chan llm.StreamEvent, <-chan error) {
	events := make(chan llm.StreamEvent)
	errs := make(chan error)
	go func() {
		defer close(events)
		defer close(errs)
		events <- llm.TextDelta{Text: "partial"}
		<-ctx.Done()
		events <- llm.StreamEnd{StopReason: "end_turn"}
		close(p.drained)
	}()
	return events, errs
}
