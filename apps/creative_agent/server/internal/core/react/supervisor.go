package react

import (
	"context"
	"fmt"
	"time"
)

// Supervisor is the zero-business ReAct loop engine.
// It drives the for-step loop: call LLM → Strategy decides → dispatch by the five branches.
// All business behavior is injected via LLM, Tools, Strategy, and Hooks.
type Supervisor struct {
	maxStep  int
	maxRelay int
	maxRetry int

	llm      LLM
	tools    Tools
	strategy Strategy
	hooks    Hooks

	// streamCallback is the streaming callback handle: when non-nil the LLM uses ChatStream and pushes chunk by chunk (side channel).
	// When nil, LLM implementations degrade to synchronous calls, equivalent to Chat.
	streamCallback StreamCallbackFunc

	prepare func(query string) []Message
}

// SetMaxStep sets the maximum ReAct loop steps.
func (s *Supervisor) SetMaxStep(n int) { s.maxStep = n }

// SetMaxRelay sets the maximum number of server-side self-Q&A relays.
func (s *Supervisor) SetMaxRelay(n int) { s.maxRelay = n }

// SetMaxRetry sets the maximum consecutive retries per step.
func (s *Supervisor) SetMaxRetry(n int) { s.maxRetry = n }

// SetLLM injects the LLM provider.
func (s *Supervisor) SetLLM(l LLM) { s.llm = l }

// SetTools injects the tool executor.
func (s *Supervisor) SetTools(t Tools) { s.tools = t }

// SetStrategy injects the decision strategy.
func (s *Supervisor) SetStrategy(st Strategy) { s.strategy = st }

// SetHooks injects the lifecycle hooks.
func (s *Supervisor) SetHooks(h Hooks) { s.hooks = h }

// SetStreamCallback injects the streaming callback handle. When non-nil the main loop uses ChatStream and pushes chunk by chunk (side channel).
func (s *Supervisor) SetStreamCallback(cb StreamCallbackFunc) { s.streamCallback = cb }

// SetPrepare injects the prompt preparation function.
func (s *Supervisor) SetPrepare(p func(query string) []Message) { s.prepare = p }

// Run executes the ReAct main loop. It returns the final Agent result, or an error when limits are exceeded.
func (s *Supervisor) Run(c *AgentContext) (*AgentResult, error) {
	prepareQuery := c.Query
	relayCount := 0
	retryCount := make(map[int]int)

preparePrompt:
	if len(c.Runtime.Messages) == 0 {
		c.Runtime.Messages = s.prepare(prepareQuery)
	}

	for ; c.Runtime.Step < s.maxStep; c.Runtime.Step++ {
		step := c.Runtime.Step

		// 1. Fetch tools (re-fetched every round, supporting dynamic changes)
		tools := s.strategy.GetTools(c)

		// 2. Call the LLM (always via ChatStream: when streamCallback is non-nil it pushes streaming side-channel frames;
		//    when nil, LLMs degrade to synchronous calls, equivalent to Chat)
		llmCtx := c.Ctx
		if llmCtx == nil {
			llmCtx = context.Background()
		}
		resp, err := s.llm.ChatStream(llmCtx, c.Runtime.Messages, tools, s.streamCallback)
		if err != nil {
			return nil, fmt.Errorf("core: llm chat at step %d: %w", step+1, err)
		}
		c.Runtime.LastResponse = resp

		// 2.5 Round-level snapshot: lets OnAfterLLM persist model_input / start_time into the thought table.
		// Must be stored before appending to messages, ensuring model_input is exactly what the LLM saw.
		inputSnapshot := make([]Message, len(c.Runtime.Messages))
		copy(inputSnapshot, c.Runtime.Messages)
		c.Set(LLMInputMessagesKey, inputSnapshot)
		c.Set(LLMStepStartKey, time.Now().UnixMilli())

		// When the LLM returned tool_calls, append the assistant message to history first
		if len(resp.ToolCalls) > 0 {
			c.Runtime.Messages = append(c.Runtime.Messages, Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})
		}

		// 3. Decide
		decision := s.strategy.Decide(c)
		s.hooks.OnAfterLLM(c)
		if decision == nil {
			return nil, fmt.Errorf("core: strategy.Decide returned nil at step %d", step+1)
		}

		// 4. Dispatch by Action
		switch decision.Action {

		case ActionTerminal:
			s.hooks.OnBeforeTerminal(c)
			reply, _ := decision.Metadata["friendly_reply"].(string)
			return &AgentResult{Content: reply, StepCount: step + 1, Ext: decision.Metadata}, nil

		case ActionInterrupt:
			// HIL breakpoint: save the checkpoint and return; the next Run resumes from it.
			s.hooks.OnBeforeInterrupt(c)
			reply, _ := decision.Metadata["friendly_reply"].(string)
			// The checkpoint stores the full structure (messages + step) so RunResume can restore directly.
			// Note: Messages shares the underlying array with c.Runtime.Messages; after the interrupt Run returns immediately,
			// so appends on the resume side are safe as long as they do not exceed the len recorded in the checkpoint.
			c.Set(CheckpointKey, &Checkpoint{
				Messages: c.Runtime.Messages,
				Step:     step + 1,
				Query:    c.Query,
			})
			return &AgentResult{
				Content:     reply,
				StepCount:   step + 1,
				Interrupted: true,
			}, nil

		case ActionRelay:
			// Server-side self-Q&A: inject a new query, clear messages, goto to rebuild the prompt
			s.hooks.OnBeforeRelay(c)
			relayCount++
			if relayCount > s.maxRelay {
				return nil, fmt.Errorf("core: relay exceeded max (%d)", s.maxRelay)
			}
			relayMsg, _ := decision.Metadata["relay_message"].(string)
			prepareQuery = relayMsg
			c.Runtime.Messages = nil
			c.Runtime.Step = 0 // restart counting after a relay
			retryCount = make(map[int]int)
			goto preparePrompt

		case ActionRetry:
			// Non-conforming output: inject a correction hint; step-- cancels the for-loop's step++ (no step consumed)
			retryCount[step]++
			if retryCount[step] > s.maxRetry {
				return nil, fmt.Errorf("core: retry limit exceeded at step %d", step+1)
			}
			retryMsg, _ := decision.Metadata["retry_message"].(string)
			if retryMsg == "" {
				retryMsg = "输出格式不符合要求，请重新按规定格式输出。"
			}
			// Remove the faulty assistant message and inject the correction hint
			if len(resp.ToolCalls) > 0 && len(c.Runtime.Messages) > 0 &&
				c.Runtime.Messages[len(c.Runtime.Messages)-1].Role == "assistant" {
				c.Runtime.Messages = c.Runtime.Messages[:len(c.Runtime.Messages)-1]
			}
			c.Runtime.Messages = append(c.Runtime.Messages, Message{Role: "user", Content: retryMsg})
			c.Runtime.Step-- // cancel the for-loop's step++
			continue

		case ActionContinue:
			// The Act of ReAct: execute the tool and feed the result back as an observation
			for _, tc := range resp.ToolCalls {
				result, handled, err := s.tools.Execute(tc.Name, tc.Args)
				if err != nil {
					// The essence: tool failures do not break the loop; the error becomes an observation so the LLM decides what to do
					c.Runtime.Messages = append(c.Runtime.Messages, Message{
						Role:       "tool",
						Content:    fmt.Sprintf("工具执行失败: %v", err),
						ToolCallID: tc.ID,
						Name:       tc.Name,
					})
					continue
				}
				if !handled {
					result = "ok"
				}
				c.Runtime.Messages = append(c.Runtime.Messages, Message{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
					Name:       tc.Name,
				})
			}
			continue
		}
	}
	return nil, fmt.Errorf("core: max step limit exceeded (%d)", s.maxStep)
}
