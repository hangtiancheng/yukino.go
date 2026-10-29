// Package appkitllm provides LLM injection implementations: the real LLM based on eino ChatModel
// (OpenAI-compatible / Anthropic Claude) and the global injection points (DefaultRealLLM / DefaultManagerLLM).
package appkitllm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cloudwego/eino-ext/components/model/claude"
	einopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// ToolChoice values.
const (
	ToolChoiceAuto     = ""         // default: the model freely decides whether to call tools
	ToolChoiceRequired = "required" // forced function calling
)

// Provider values.
const (
	ProviderOpenAI = "openai" // OpenAI-compatible /chat/completions (DeepSeek, etc.), default
	ProviderClaude = "claude" // Anthropic Claude
)

// Config is the configuration of the eino LLM.
type Config struct {
	Provider string // "openai" (default) | "claude"
	BaseURL  string // OpenAI-compatible API address / Claude proxy address
	APIKey   string
	Model    string // model name, e.g. deepseek-chat / claude-sonnet-4-5
	// ToolChoice forced-function-calling switch: "" (default auto) | "required"
	ToolChoice string
	Timeout    time.Duration
}

// EinoLLM implements react.LLM on top of the eino ChatModel component.
// Zero engine changes — just swap it in at the injection point.
type EinoLLM struct {
	chat       model.ToolCallingChatModel
	modelName  string
	toolChoice string
}

// NewEinoLLM creates an eino ChatModel from the config and wraps it as react.LLM.
func NewEinoLLM(cfg Config) (*EinoLLM, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	// OTel outbound tracing (the global provider is set by otelx.Init).
	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	provider := cfg.Provider
	if provider == "" {
		provider = ProviderOpenAI
	}

	var chat model.ToolCallingChatModel
	switch provider {
	case ProviderClaude:
		claudeCfg := &claude.Config{
			APIKey:         cfg.APIKey,
			Model:          cfg.Model,
			MaxTokens:      4096,
			HTTPClient:     httpClient,
			RequestTimeout: timeout,
		}
		if cfg.BaseURL != "" {
			baseURL := cfg.BaseURL
			claudeCfg.BaseURL = &baseURL
		}
		m, err := claude.NewChatModel(context.Background(), claudeCfg)
		if err != nil {
			return nil, fmt.Errorf("eino_llm: claude chat model: %w", err)
		}
		chat = m
	default:
		openaiCfg := &einopenai.ChatModelConfig{
			APIKey:     cfg.APIKey,
			BaseURL:    cfg.BaseURL,
			Model:      cfg.Model,
			HTTPClient: httpClient,
		}
		// thinking mode does not support tool_choice: forced function calling must disable
		// thinking, otherwise 400 (ExtraFields passes vendor extension fields through). Vendor conventions differ:
		//   - DeepSeek: thinking.type=disabled
		//   - Qwen (idealab / DashScope compatible): enable_thinking=false
		// Both fields are passed through together; each vendor ignores the fields it does not recognize.
		if cfg.ToolChoice == ToolChoiceRequired {
			openaiCfg.ExtraFields = map[string]any{
				"thinking":        map[string]any{"type": "disabled"},
				"enable_thinking": false,
			}
		}
		m, err := einopenai.NewChatModel(context.Background(), openaiCfg)
		if err != nil {
			return nil, fmt.Errorf("eino_llm: openai chat model: %w", err)
		}
		chat = m
	}

	return &EinoLLM{
		chat:       chat,
		modelName:  cfg.Model,
		toolChoice: cfg.ToolChoice,
	}, nil
}

// Chat implements the react.LLM interface (non-streaming).
func (l *EinoLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	out, err := l.chat.Generate(ctx, toEinoMessages(messages), l.options(tools)...)
	if err != nil {
		return nil, fmt.Errorf("eino_llm: generate: %w", err)
	}
	return fromEinoMessage(out), nil
}

// ChatStream implements the streaming call of the react.LLM interface.
//
// When callback is non-nil: subscribes to the eino StreamReader and calls back once per chunk;
// at the end it triggers isEndingChunk=true (including usage). The return value matches Chat (the complete response, for ReAct decisions).
// When callback is nil it degrades to synchronous Chat (equivalent behavior).
func (l *EinoLLM) ChatStream(ctx context.Context, messages []react.Message, tools []react.FunctionDef, callback react.StreamCallbackFunc) (*react.LLMResponse, error) {
	if callback == nil {
		return l.Chat(ctx, messages, tools)
	}

	stream, err := l.chat.Stream(ctx, toEinoMessages(messages), l.options(tools)...)
	if err != nil {
		return nil, fmt.Errorf("eino_llm: stream: %w", err)
	}
	defer stream.Close()

	acc := newStreamAccumulator()
	var usage react.StreamedChatUsage
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("eino_llm: stream recv: %w", err)
		}
		if u := usageFromChunk(chunk); u != nil {
			usage = *u
		}
		if err := callback(ctx, chunkToPayload(l.modelName, chunk), false); err != nil {
			return nil, err
		}
		acc.add(chunk)
	}

	final := chunkToPayload(l.modelName, nil)
	final.Usage = usage
	if err := callback(ctx, final, true); err != nil {
		return nil, err
	}
	return acc.response(), nil
}

// options assembles the eino call options: tool list + forced function calling.
func (l *EinoLLM) options(tools []react.FunctionDef) []model.Option {
	if len(tools) == 0 {
		return nil
	}
	opts := []model.Option{model.WithTools(toEinoTools(tools))}
	// tool_choice is only allowed when tools are declared (consistent with the OpenAI contract).
	if l.toolChoice == ToolChoiceRequired {
		opts = append(opts, model.WithToolChoice(schema.ToolChoiceForced))
	}
	return opts
}

// toEinoMessages converts react.Message → eino schema.Message.
func toEinoMessages(msgs []react.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		msg := &schema.Message{
			Role:       schema.RoleType(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.Name,
		}
		for _, tc := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      tc.Name,
					Arguments: tc.Args,
				},
			})
		}
		out = append(out, msg)
	}
	return out
}

// fromEinoMessage converts eino schema.Message → react.LLMResponse.
func fromEinoMessage(msg *schema.Message) *react.LLMResponse {
	result := &react.LLMResponse{}
	if msg == nil {
		return result
	}
	result.Content = msg.Content
	for _, tc := range msg.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, react.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: tc.Function.Arguments,
		})
	}
	return result
}

// toEinoTools converts react.FunctionDef → eino schema.ToolInfo.
func toEinoTools(tools []react.FunctionDef) []*schema.ToolInfo {
	out := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		info := &schema.ToolInfo{
			Name: t.Name,
			Desc: t.Description,
		}
		if len(t.Parameters.Properties) > 0 {
			required := make(map[string]bool, len(t.Parameters.Required))
			for _, name := range t.Parameters.Required {
				required[name] = true
			}
			params := make(map[string]*schema.ParameterInfo, len(t.Parameters.Properties))
			for name, prop := range t.Parameters.Properties {
				params[name] = &schema.ParameterInfo{
					Type:     schema.DataType(prop.Type),
					Desc:     prop.Description,
					Enum:     prop.Enum,
					Required: required[name],
				}
			}
			info.ParamsOneOf = schema.NewParamsOneOfByParams(params)
		}
		out = append(out, info)
	}
	return out
}

// usageFromChunk extracts the token statistics carried by the final chunk.
func usageFromChunk(msg *schema.Message) *react.StreamedChatUsage {
	if msg == nil || msg.ResponseMeta == nil || msg.ResponseMeta.Usage == nil {
		return nil
	}
	u := msg.ResponseMeta.Usage
	return &react.StreamedChatUsage{
		CompletionTokens: int64(u.CompletionTokens),
		PromptTokens:     int64(u.PromptTokens),
		TotalTokens:      int64(u.TotalTokens),
	}
}

// chunkToPayload converts an eino chunk → react.StreamedChatResponsePayload.
// A nil chunk returns an empty payload (for the final isEndingChunk=true callback carrying usage).
func chunkToPayload(modelName string, chunk *schema.Message) *react.StreamedChatResponsePayload {
	payload := &react.StreamedChatResponsePayload{
		Model:  modelName,
		Object: "chat.completion.chunk",
	}
	if chunk == nil {
		return payload
	}
	delta := react.StreamedChatDelta{
		Role:             string(chunk.Role),
		Content:          chunk.Content,
		ReasoningContent: chunk.ReasoningContent,
	}
	for _, tc := range chunk.ToolCalls {
		delta.ToolCalls = append(delta.ToolCalls, react.StreamedToolCall{
			Index: tc.Index,
			ID:    tc.ID,
			Type:  tc.Type,
			Function: react.StreamedToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	payload.Choices = []react.StreamedChatChoice{{Delta: delta}}
	return payload
}

// streamAccumulator accumulates streaming chunks into the complete response (content concatenation + tool_call merging by index).
type streamAccumulator struct {
	content   string
	toolCalls []react.ToolCall
	indexes   map[int]int // tool_call index → position in toolCalls
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{indexes: make(map[int]int)}
}

func (a *streamAccumulator) add(chunk *schema.Message) {
	if chunk == nil {
		return
	}
	a.content += chunk.Content
	for _, tc := range chunk.ToolCalls {
		idx := -1
		if tc.Index != nil {
			idx = *tc.Index
		}
		pos, ok := a.indexes[idx]
		if !ok {
			pos = len(a.toolCalls)
			a.indexes[idx] = pos
			a.toolCalls = append(a.toolCalls, react.ToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: tc.Function.Arguments,
			})
			continue
		}
		// Later chunks contain only arguments deltas (OpenAI streaming contract); merge and accumulate by index.
		if tc.ID != "" {
			a.toolCalls[pos].ID = tc.ID
		}
		if tc.Function.Name != "" {
			a.toolCalls[pos].Name = tc.Function.Name
		}
		a.toolCalls[pos].Args += tc.Function.Arguments
	}
}

func (a *streamAccumulator) response() *react.LLMResponse {
	return &react.LLMResponse{
		Content:   a.content,
		ToolCalls: a.toolCalls,
	}
}
