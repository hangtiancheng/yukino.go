package plan_execute_replan

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/cloudwego/eino/adk"
	plan_execute "github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/models"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

type replanResponse struct {
	Done      bool     `json:"done"`
	Remaining []string `json:"remaining"`
	Summary   string   `json:"summary"`
}

func NewRePlanAgent(ctx context.Context, cfg *config.Config) (adk.Agent, error) {
	chatModel, err := models.NewThinkChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return &customReplanner{
		chatModel: chatModel,
	}, nil
}

type customReplanner struct {
	chatModel model.ToolCallingChatModel
}

func (r *customReplanner) Name(_ context.Context) string {
	return "replanner"
}

func (r *customReplanner) Description(_ context.Context) string {
	return "a replanner agent that uses structured output"
}

func (r *customReplanner) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer func() {
			panicErr := recover()
			if panicErr != nil {
				e := fmt.Errorf("panic in replanner: %v\n%s", panicErr, debug.Stack())
				generator.Send(&adk.AgentEvent{Err: e})
			}
			generator.Close()
		}()

		executedStep, ok := adk.GetSessionValue(ctx, plan_execute.ExecutedStepSessionKey)
		if !ok {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("executed step not found")})
			return
		}
		executedStepStr := executedStep.(string)

		plan, ok := adk.GetSessionValue(ctx, plan_execute.PlanSessionKey)
		if !ok {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("plan not found")})
			return
		}
		planObj := plan.(plan_execute.Plan)

		var executedSteps []plan_execute.ExecutedStep
		executedStepsVal, ok := adk.GetSessionValue(ctx, plan_execute.ExecutedStepsSessionKey)
		if ok {
			executedSteps = executedStepsVal.([]plan_execute.ExecutedStep)
		}

		executedSteps = append(executedSteps, plan_execute.ExecutedStep{
			Step:   planObj.FirstStep(),
			Result: executedStepStr,
		})
		adk.AddSessionValue(ctx, plan_execute.ExecutedStepsSessionKey, executedSteps)

		userInput, ok := adk.GetSessionValue(ctx, plan_execute.UserInputSessionKey)
		if !ok {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("user input not found")})
			return
		}
		userInputMsgs := userInput.([]adk.Message)

		var userInputStr strings.Builder
		for _, msg := range userInputMsgs {
			userInputStr.WriteString(msg.Content)
			userInputStr.WriteString("\n")
		}

		planBytes, _ := planObj.MarshalJSON()

		var completedStepsStr strings.Builder
		var resultsStr strings.Builder
		for i, step := range executedSteps {
			fmt.Fprintf(&completedStepsStr, "%d. %s\n", i+1, step.Step)
			resultsStr.WriteString(step.Result)
			resultsStr.WriteString("\n")
		}

		promptText := fmt.Sprintf(`You are a replanning agent reviewing execution progress toward an objective. Analyze the completed steps and their outcomes to decide whether the objective is fully achieved or further action is required.

Task:
%s

Original Plan:
%s

Completed Steps:
%s

Results So Far:
%s

Based on the progress above, respond with ONLY a JSON object matching this schema:
{
  "done": <boolean>,
  "remaining": ["<step>", ...],
  "summary": "<final report when done, otherwise empty string>"
}

Set "done" to true and provide a comprehensive summary only when the objective is fully achieved. Otherwise, set "done" to false and list only the remaining steps. Do not include any text, explanations, or markdown formatting outside the JSON object.`,
			userInputStr.String(), string(planBytes), completedStepsStr.String(), resultsStr.String())

		msgs := []*schema.Message{schema.UserMessage(promptText)}
		resp, err := r.chatModel.Generate(ctx, msgs)
		if err != nil {
			generator.Send(&adk.AgentEvent{Err: err})
			return
		}

		clean, extractErr := extractJSONObject(resp.Content)
		if extractErr != nil {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("extract replan JSON: %w (raw output: %q)", extractErr, resp.Content)})
			return
		}
		var replanResp replanResponse
		if err := json.Unmarshal([]byte(clean), &replanResp); err != nil {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("failed to parse replan response: %w", err)})
			return
		}

		if replanResp.Done {
			output := schema.AssistantMessage(replanResp.Summary, nil)
			generator.Send(adk.EventFromMessage(output, nil, schema.Assistant, ""))
			generator.Send(&adk.AgentEvent{Action: adk.NewBreakLoopAction(r.Name(ctx))})
		} else {
			newPlan := &customPlan{Steps: replanResp.Remaining}
			adk.AddSessionValue(ctx, plan_execute.PlanSessionKey, newPlan)

			planJSON, _ := newPlan.MarshalJSON()
			output := schema.AssistantMessage(string(planJSON), nil)
			generator.Send(adk.EventFromMessage(output, nil, schema.Assistant, ""))
		}
	}()

	return iterator
}
