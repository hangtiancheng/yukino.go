package plan_execute_replan

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/adk"
	plan_execute "github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

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
		return nil, fmt.Errorf("extract plan JSON: %w (raw output: %q)", extractErr, resp.Content)
	}
	return schema.AssistantMessage(clean, resp.ToolCalls), nil
}

func NewPlanner(ctx context.Context, cfg *config.Config) (adk.Agent, error) {
	planModel, err := models.NewThinkChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}

	wrappedModel := &structuredOutputModel{ToolCallingChatModel: planModel}

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
