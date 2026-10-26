package takeout

import (
	"context"
	"fmt"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

// AgentReply is the normalized result of one LLM call.
type AgentReply struct {
	Text         string
	InputTokens  int64
	OutputTokens int64
	HasUsage     bool
}

// TotalTokens returns the summed token usage, or nil when the provider did
// not report usage.
func (r *AgentReply) TotalTokens() *int64 {
	if !r.HasUsage {
		return nil
	}
	total := r.InputTokens + r.OutputTokens
	return &total
}

// Agent is a stateless worker: a system prompt bound to a chat model. The
// Java version cached one ReActAgent per session and cleared its memory
// before every call; because each call here is a single stateless turn,
// one shared instance per agent role is equivalent.
type Agent struct {
	Name       string
	ModelName  string
	systemText string
	chatModel  *model.DashScopeChatModel
	// timeout bounds a single call; zero disables the bound. A bound keeps a
	// hung provider from holding the orchestrator's per-session lock forever.
	timeout time.Duration
}

// Call sends one system+user turn to the model and returns the text reply
// with token usage.
func (a *Agent) Call(ctx context.Context, userPrompt string) (*AgentReply, error) {
	if a.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.timeout)
		defer cancel()
	}
	msgs := []*message.Msg{
		message.SystemMsg(a.Name, a.systemText),
		message.UserMsg("user", userPrompt),
	}
	response, err := a.chatModel.Chat(ctx, msgs)
	if err != nil {
		return nil, fmt.Errorf("agent %s call failed: %w", a.Name, err)
	}
	reply := &AgentReply{Text: response.GetTextContent()}
	if response.Usage != nil {
		reply.HasUsage = true
		reply.InputTokens = int64(response.Usage.InputTokens)
		reply.OutputTokens = int64(response.Usage.OutputTokens)
	}
	return reply, nil
}

// ModelSet holds the two DashScope chat models used across agents: a main
// model for recommendation/plan responses and a light model for intent,
// clarify, and judging.
type ModelSet struct {
	MainModelName  string
	LightModelName string
	main           *model.DashScopeChatModel
	light          *model.DashScopeChatModel
}

// NewModelSet creates the main and light DashScope chat models.
func NewModelSet(apiKey, baseURL, mainModelName, lightModelName string) (*ModelSet, error) {
	if apiKey == "" {
		return nil, NewError("DashScope API key is required (set llm.api_key or the env var named by llm.api_key_env)")
	}
	mainModel, err := model.NewDashScopeChatModel(model.DashScopeConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   mainModelName,
	})
	if err != nil {
		return nil, WrapError(err, "create main chat model failed")
	}
	lightModel, err := model.NewDashScopeChatModel(model.DashScopeConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   lightModelName,
	})
	if err != nil {
		return nil, WrapError(err, "create light chat model failed")
	}
	return &ModelSet{
		MainModelName:  mainModelName,
		LightModelName: lightModelName,
		main:           mainModel,
		light:          lightModel,
	}, nil
}

// AgentSet is the fixed set of worker agents used by the orchestrator and
// the evaluation pipeline.
type AgentSet struct {
	Intent            *Agent
	Clarify           *Agent
	RecommendResponse *Agent
	PlanResponse      *Agent
	EvaluationJudge   *Agent
}

// NewAgentSet builds every agent from the embedded English prompts.
// callTimeout bounds each LLM call; zero disables the bound.
func NewAgentSet(models *ModelSet, loader PromptLoader, callTimeout time.Duration) *AgentSet {
	return &AgentSet{
		Intent: &Agent{
			Name:       "takeout_intent_agent",
			ModelName:  models.LightModelName,
			systemText: loader.MustLoad("prompts/intent.txt"),
			chatModel:  models.light,
			timeout:    callTimeout,
		},
		Clarify: &Agent{
			Name:       "takeout_clarify_agent",
			ModelName:  models.LightModelName,
			systemText: loader.MustLoad("prompts/clarify.txt"),
			chatModel:  models.light,
			timeout:    callTimeout,
		},
		RecommendResponse: &Agent{
			Name:       "takeout_recommend_response_agent",
			ModelName:  models.MainModelName,
			systemText: loader.MustLoad("prompts/recommend_response.txt"),
			chatModel:  models.main,
			timeout:    callTimeout,
		},
		PlanResponse: &Agent{
			Name:       "takeout_plan_response_agent",
			ModelName:  models.MainModelName,
			systemText: loader.MustLoad("prompts/plan_response.txt"),
			chatModel:  models.main,
			timeout:    callTimeout,
		},
		EvaluationJudge: &Agent{
			Name:       "takeout_evaluation_judge_agent",
			ModelName:  models.LightModelName,
			systemText: loader.MustLoad("prompts/evaluation_judge.txt"),
			chatModel:  models.light,
			timeout:    callTimeout,
		},
	}
}
