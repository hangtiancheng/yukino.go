package react

import (
	"context"
	"fmt"
	"time"
)

// Supervisor 是零业务的 ReAct 循环引擎。
// 驱动 for-step 循环：调 LLM → Strategy 决策 → 按五分支分发。
// 所有业务行为通过 LLM、Tools、Strategy、Hooks 注入。
type Supervisor struct {
	maxStep  int
	maxRelay int
	maxRetry int

	llm      LLM
	tools    Tools
	strategy Strategy
	hooks    Hooks

	// streamCallback 是流式回调句柄：非 nil 时 LLM 走 ChatStream 逐 chunk 推送（旁路）。
	// nil 时各 LLM 实现降级为同步调用，行为与 Chat 等价。
	streamCallback StreamCallbackFunc

	prepare func(query string) []Message
}

// SetMaxStep 设置 ReAct 最大循环步数。
func (s *Supervisor) SetMaxStep(n int) { s.maxStep = n }

// SetMaxRelay 设置服务端自问自答最大次数。
func (s *Supervisor) SetMaxRelay(n int) { s.maxRelay = n }

// SetMaxRetry 设置每步最大连续重试次数。
func (s *Supervisor) SetMaxRetry(n int) { s.maxRetry = n }

// SetLLM 注入 LLM 提供者。
func (s *Supervisor) SetLLM(l LLM) { s.llm = l }

// SetTools 注入工具执行器。
func (s *Supervisor) SetTools(t Tools) { s.tools = t }

// SetStrategy 注入决策策略。
func (s *Supervisor) SetStrategy(st Strategy) { s.strategy = st }

// SetHooks 注入生命周期回调。
func (s *Supervisor) SetHooks(h Hooks) { s.hooks = h }

// SetStreamCallback 注入流式回调句柄。非 nil 时主循环走 ChatStream 逐 chunk 推送（旁路）。
func (s *Supervisor) SetStreamCallback(cb StreamCallbackFunc) { s.streamCallback = cb }

// SetPrepare 注入 prompt 准备函数。
func (s *Supervisor) SetPrepare(p func(query string) []Message) { s.prepare = p }

// Run 执行 ReAct 主循环。返回最终的 Agent 结果，或超限时返回错误。
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

		// 1. 取工具（每轮重取，支持动态变化）
		tools := s.strategy.GetTools(c)

		// 2. 调 LLM（统一走 ChatStream：streamCallback 非 nil 时流式旁路推送，
		//    nil 时各 LLM 降级为同步调用，行为与 Chat 等价）
		llmCtx := c.Ctx
		if llmCtx == nil {
			llmCtx = context.Background()
		}
		resp, err := s.llm.ChatStream(llmCtx, c.Runtime.Messages, tools, s.streamCallback)
		if err != nil {
			return nil, fmt.Errorf("core: llm chat at step %d: %w", step+1, err)
		}
		c.Runtime.LastResponse = resp

		// 2.5 轮级快照：供 OnAfterLLM 落 thought 表取 model_input / start_time。
		// 必须在 messages append 之前存，保证 model_input 是 LLM 实际看到的输入。
		inputSnapshot := make([]Message, len(c.Runtime.Messages))
		copy(inputSnapshot, c.Runtime.Messages)
		c.Set(LLMInputMessagesKey, inputSnapshot)
		c.Set(LLMStepStartKey, time.Now().UnixMilli())

		// LLM 返回了 tool_calls 时先把 assistant 消息落进历史
		if len(resp.ToolCalls) > 0 {
			c.Runtime.Messages = append(c.Runtime.Messages, Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})
		}

		// 3. 决策
		decision := s.strategy.Decide(c)
		s.hooks.OnAfterLLM(c)
		if decision == nil {
			return nil, fmt.Errorf("core: strategy.Decide returned nil at step %d", step+1)
		}

		// 4. 按 Action 分发
		switch decision.Action {

		case ActionTerminal:
			s.hooks.OnBeforeTerminal(c)
			reply, _ := decision.Metadata["friendly_reply"].(string)
			return &AgentResult{Content: reply, StepCount: step + 1, Ext: decision.Metadata}, nil

		case ActionInterrupt:
			// HIL 断点：保存 checkpoint 后返回，下次 Run 从断点恢复
			s.hooks.OnBeforeInterrupt(c)
			reply, _ := decision.Metadata["friendly_reply"].(string)
			// checkpoint 存完整结构（messages + step），供 RunResume 直接恢复。
			// 注意：Messages 与 c.Runtime.Messages 共享底层数组；中断后 Run 立即返回，
			// resume 侧 append 不超过 checkpoint 记录的 len 即安全。
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
			// 服务端自问自答：注入新 query，清空 messages，goto 重建 prompt
			s.hooks.OnBeforeRelay(c)
			relayCount++
			if relayCount > s.maxRelay {
				return nil, fmt.Errorf("core: relay exceeded max (%d)", s.maxRelay)
			}
			relayMsg, _ := decision.Metadata["relay_message"].(string)
			prepareQuery = relayMsg
			c.Runtime.Messages = nil
			c.Runtime.Step = 0 // relay 后重头计数
			retryCount = make(map[int]int)
			goto preparePrompt

		case ActionRetry:
			// 输出不合规：注入纠错提示，step-- 抵消 for 的 step++（不消耗步数）
			retryCount[step]++
			if retryCount[step] > s.maxRetry {
				return nil, fmt.Errorf("core: retry limit exceeded at step %d", step+1)
			}
			retryMsg, _ := decision.Metadata["retry_message"].(string)
			if retryMsg == "" {
				retryMsg = "输出格式不符合要求，请重新按规定格式输出。"
			}
			// 清除错误的 assistant 消息，注入纠错提示
			if len(resp.ToolCalls) > 0 && len(c.Runtime.Messages) > 0 &&
				c.Runtime.Messages[len(c.Runtime.Messages)-1].Role == "assistant" {
				c.Runtime.Messages = c.Runtime.Messages[:len(c.Runtime.Messages)-1]
			}
			c.Runtime.Messages = append(c.Runtime.Messages, Message{Role: "user", Content: retryMsg})
			c.Runtime.Step-- // 抵消 for 的 step++
			continue

		case ActionContinue:
			// ReAct 的 Act：执行工具，结果作为 observation 塞回
			for _, tc := range resp.ToolCalls {
				result, handled, err := s.tools.Execute(tc.Name, tc.Args)
				if err != nil {
					// 精髓：工具失败不中断循环，错误当 observation 让 LLM 自己决策
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
	return nil, fmt.Errorf("core: 超出最大步数限制 (%d)", s.maxStep)
}
