// Package appkitllm 提供 LLM 注入实现：基于 eino ChatModel 的真实 LLM
// （OpenAI 兼容 / Anthropic Claude）与全局注入点（DefaultRealLLM / DefaultManagerLLM）。
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

// ToolChoice 取值。
const (
	ToolChoiceAuto     = ""         // 默认：模型自由决定是否调工具
	ToolChoiceRequired = "required" // 强制 Function Calling
)

// Provider 取值。
const (
	ProviderOpenAI = "openai" // OpenAI 兼容 /chat/completions（DeepSeek 等），默认
	ProviderClaude = "claude" // Anthropic Claude
)

// Config 是 eino LLM 的配置。
type Config struct {
	Provider string // "openai"（默认）| "claude"
	BaseURL  string // OpenAI 兼容 API 地址 / Claude 代理地址
	APIKey   string
	Model    string // 模型名，如 deepseek-chat / claude-sonnet-4-5
	// ToolChoice 强制 Function Calling 开关：""（默认 auto）| "required"
	ToolChoice string
	Timeout    time.Duration
}

// EinoLLM 实现 react.LLM，底层为 eino ChatModel 组件。
// 引擎零改动——只需在注入点替换。
type EinoLLM struct {
	chat       model.ToolCallingChatModel
	modelName  string
	toolChoice string
}

// NewEinoLLM 按配置创建 eino ChatModel 并包装为 react.LLM。
func NewEinoLLM(cfg Config) (*EinoLLM, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	// OTel 出站追踪（全局 provider 由 otelx.Init 设置）。
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
		// thinking 模式不支持 tool_choice：强制 Function Calling 时必须禁用
		// thinking，否则 400（ExtraFields 透传厂商扩展字段）。各厂商约定不同：
		//   - DeepSeek：thinking.type=disabled
		//   - Qwen（idealab / DashScope 兼容）：enable_thinking=false
		// 两个字段同时透传，厂商各自忽略不认识的字段。
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

// Chat 实现 react.LLM 接口（非流式）。
func (l *EinoLLM) Chat(ctx context.Context, messages []react.Message, tools []react.FunctionDef) (*react.LLMResponse, error) {
	out, err := l.chat.Generate(ctx, toEinoMessages(messages), l.options(tools)...)
	if err != nil {
		return nil, fmt.Errorf("eino_llm: generate: %w", err)
	}
	return fromEinoMessage(out), nil
}

// ChatStream 实现 react.LLM 接口的流式调用。
//
// callback 非 nil 时：订阅 eino StreamReader，每个 chunk 回调一次；
// 末尾触发 isEndingChunk=true（含 usage）。返回值与 Chat 相同（完整响应，供 ReAct 决策）。
// callback 为 nil 时降级为同步 Chat（行为等价）。
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

// options 组装 eino 调用选项：工具列表 + 强制 Function Calling。
func (l *EinoLLM) options(tools []react.FunctionDef) []model.Option {
	if len(tools) == 0 {
		return nil
	}
	opts := []model.Option{model.WithTools(toEinoTools(tools))}
	// 仅在声明了工具时才允许 tool_choice（与 OpenAI 契约一致）。
	if l.toolChoice == ToolChoiceRequired {
		opts = append(opts, model.WithToolChoice(schema.ToolChoiceForced))
	}
	return opts
}

// toEinoMessages 转换 react.Message → eino schema.Message。
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

// fromEinoMessage 转换 eino schema.Message → react.LLMResponse。
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

// toEinoTools 转换 react.FunctionDef → eino schema.ToolInfo。
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

// usageFromChunk 提取 chunk 末包携带的 token 统计。
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

// chunkToPayload 转换 eino chunk → react.StreamedChatResponsePayload。
// chunk 为 nil 时返回空载荷（供末包 isEndingChunk=true 携带 usage 用）。
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

// streamAccumulator 累积流式 chunks 为完整响应（content 拼接 + tool_call 按 index 归并）。
type streamAccumulator struct {
	content   string
	toolCalls []react.ToolCall
	indexes   map[int]int // tool_call index → toolCalls 下标
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
		// 后续 chunk 仅含 arguments 增量（OpenAI 流式契约），按 index 归并累积。
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
