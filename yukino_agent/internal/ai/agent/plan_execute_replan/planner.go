// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package plan_execute_replan

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/adk"
	plan_execute "github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
)

// structuredOutputModel wraps a ToolCallingChatModel so that it behaves as a
// BaseChatModel emitting clean JSON content. It is needed because some models
// (e.g. Qwen3.7) don't properly support Anthropic's tool_choice: forced, so the
// planner/replanner fall back to prompt-based structured output. In practice
// such models sometimes wrap the JSON in ```json fences or prepend reasoning;
// Generate strips those by running extractJSONObject over the raw content so
// that the downstream plan.UnmarshalJSON always sees a clean object.
//
// ToolCallingChatModel already satisfies BaseChatModel; the wrapper exists only
// to post-process the content (a transparent pass-through would be dead code).
type structuredOutputModel struct {
	model.ToolCallingChatModel
}

func (m *structuredOutputModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	resp, err := m.ToolCallingChatModel.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	clean, extractErr := extractJSONObject(resp.Content)
	if extractErr != nil {
		// Surface the parse failure with the raw content for debugging.
		return nil, fmt.Errorf("extract plan JSON: %w (raw output: %q)", extractErr, resp.Content)
	}
	return schema.AssistantMessage(clean, resp.ToolCalls), nil
}

// NewPlanner creates the planning agent that decomposes a complex query
// into a sequence of executable steps. It uses structured output instead of
// tool calling for better compatibility with different LLM providers.
func NewPlanner(ctx context.Context, cfg *config.Config) (adk.Agent, error) {
	planModel, err := models.NewThinkChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Create a wrapper that uses structured output format
	wrappedModel := &structuredOutputModel{ToolCallingChatModel: planModel}

	// Create a custom GenInputFn that includes instructions for JSON output
	genInputFn := func(ctx context.Context, userInput []adk.Message) ([]adk.Message, error) {
		var query string
		for _, msg := range userInput {
			if msg.Role == schema.User {
				query = msg.Content
				break
			}
		}

		prompt := fmt.Sprintf(`Break down the following task into concrete steps.

Task:
%s

Respond with ONLY a JSON object in this exact format:
{
  "steps": ["step 1 description", "step 2 description", ...]
}

Do not include any other text, explanations, or markdown formatting. Only output the JSON object.`, query)

		return []*schema.Message{
			schema.UserMessage(prompt),
		}, nil
	}

	return plan_execute.NewPlanner(ctx, &plan_execute.PlannerConfig{
		ChatModelWithFormattedOutput: wrappedModel,
		GenInputFn:                   genInputFn,
		NewPlan: func(ctx context.Context) plan_execute.Plan {
			return &customPlan{}
		},
	})
}

// customPlan implements the Plan interface for structured output parsing.
type customPlan struct {
	Steps []string `json:"steps"`
}

func (p *customPlan) FirstStep() string {
	if len(p.Steps) == 0 {
		return ""
	}
	return p.Steps[0]
}

func (p *customPlan) MarshalJSON() ([]byte, error) {
	type planTyp customPlan
	return json.Marshal((*planTyp)(p))
}

func (p *customPlan) UnmarshalJSON(data []byte) error {
	type planTyp customPlan
	return json.Unmarshal(data, (*planTyp)(p))
}
