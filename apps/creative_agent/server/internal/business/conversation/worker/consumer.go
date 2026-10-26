package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	appkitfake "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/fake"
	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/fastpath"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/executor"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"
)

// Consumer is the background Worker that reads from Redis Streams and
// executes the ReAct pipeline for async conversation requests.
//
// Lifecycle: App.Run calls consumer.Start(ctx) as a goroutine; it loops
// XREADGROUP → handleConversation until ctx is cancelled.
//
// handleConversation flow (7 steps):
//  1. Redis NX lock  →  prevent duplicate execution
//  2. MySQL CAS takeover  →  Pending → InProgress
//  3. Start heartbeat goroutine  →  refresh every 5s
//  4. Run ReAct via SupervisorRunner  →  same engine as sync path!
//  5. Write EndTurn chunk  →  signals RunAttach to exit
//  6. FlushChatTextToDB  →  batch write message_record
//  7. MarkTerminal + release lock + ACK
//
// Phase 3 扩展：管家产出 new_agents_plan 时，通过 Submitter 投递子任务，
// 并用 Poller 轮询 cg_task_step 把进度推成 chunk 回流前端。
type Consumer struct {
	streamQ    infra.StreamQueue                  // 消息队列：XREADGROUP/XACK
	chunkStore infra.ChunkStore                   // chunk 中转站：Write/After
	lock       infra.Locker                       // 心跳锁（Redis 或进程内实现）
	asyncRepo  repository.AsyncRequestRepository  // cg_async_request DAO
	msgRepo    repository.MessageRecordRepository // cg_message_record DAO
	workerID   string                             // 本 worker 唯一标识
	logger     *zap.Logger

	// Phase 3: 子任务链路（nil 时跳过 plan 分支）
	taskSubmitter *subtaskexecutor.Submitter
	taskPoller    *taskflow.Poller

	// Phase 4: cron 注册（nil 时跳过）
	cronRepo repository.CronTaskRepository
	// Phase 4: 快任务回流定位 task_run（GetByMsgID）
	taskRunRepo repository.TaskRunRepository

	// 业务决策回调（由 app 装配注入）。nil 时退化为无业务接线的消费壳。
	hooks *TaskRunnerHooks
}

// ConsumerConfig holds dependencies for creating a Consumer.
type ConsumerConfig struct {
	StreamQ    infra.StreamQueue
	ChunkStore infra.ChunkStore
	// Lock is the NX locker (Redis-backed or in-memory) for heartbeat key (cg:hb:{msgID}).
	Lock      infra.Locker
	AsyncRepo repository.AsyncRequestRepository
	MsgRepo   repository.MessageRecordRepository
	// WorkerID uniquely identifies this consumer instance in the consumer group.
	// Used as the Redis Stream consumer name and heartbeat lock value.
	WorkerID string
	Logger   *zap.Logger

	// Phase 3: 子任务链路（nil 时跳过 plan 分支）。
	TaskSubmitter *subtaskexecutor.Submitter
	TaskPoller    *taskflow.Poller

	// Phase 4: cron 注册（nil 时跳过）。
	CronRepo repository.CronTaskRepository
	// Phase 4: 快任务回流定位 task_run（GetByMsgID）。
	TaskRunRepo repository.TaskRunRepository
	// Phase 4: 业务决策回调（由 app 装配注入；nil 时退化为无业务接线消费壳）。
	Hooks *TaskRunnerHooks
}

// NewConsumer creates a background Worker consumer.
func NewConsumer(cfg ConsumerConfig) *Consumer {
	return &Consumer{
		streamQ:       cfg.StreamQ,
		chunkStore:    cfg.ChunkStore,
		lock:          cfg.Lock,
		asyncRepo:     cfg.AsyncRepo,
		msgRepo:       cfg.MsgRepo,
		workerID:      cfg.WorkerID,
		logger:        cfg.Logger,
		taskSubmitter: cfg.TaskSubmitter,
		taskPoller:    cfg.TaskPoller,
		cronRepo:      cfg.CronRepo,
		taskRunRepo:   cfg.TaskRunRepo,
		hooks:         cfg.Hooks,
	}
}

// Start begins the consumer loop. It blocks until ctx is cancelled.
func (c *Consumer) Start(ctx context.Context) {
	c.logger.Info("async worker started", zap.String("workerID", c.workerID))
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("async worker stopped")
			return
		default:
		}

		msg, err := c.streamQ.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Error("worker receive error", zap.Error(err))
			continue
		}
		if msg == nil {
			continue // No message within block time
		}

		// Process in a goroutine so slow messages don't block the poll loop
		go c.handleConversation(ctx, msg)
	}
}

// conversationPayload is deserialized from the stream message body.
type conversationPayload struct {
	AsyncRequestID string `json:"async_request_id"`
	MsgID          string `json:"msg_id"`
	UserID         int64  `json:"user_id"`
	SessionID      string `json:"session_id"`
	AgentID        string `json:"agent_id"`
	Query          string `json:"query"`

	// Phase 4: 快捷路径参数（con_reader 在 stream body 中透传）
	RequestType string `json:"requestType"` // "" / "retry" / "cron"
	Resume      bool   `json:"resume"`      // HIL 断点恢复
	// OriginMsgID 点卡确认后回传的上一步消息 ID（fastpath 追溯前序脚本用）。
	OriginMsgID string `json:"originMsgId"`

	// TraceParent 是 W3C traceparent，跨进程透传 trace。
	TraceParent string `json:"traceparent,omitempty"`
}

// handleConversation processes a single async conversation request.
func (c *Consumer) handleConversation(ctx context.Context, msg *infra.StreamMessage) {
	var payload conversationPayload
	if err := json.Unmarshal(msg.Body, &payload); err != nil {
		c.logger.Error("worker: failed to unmarshal payload", zap.Error(err))
		_ = c.streamQ.Ack(ctx, msg.ID) // Poison message, ack to avoid blocking
		return
	}

	// 从 stream 恢复 trace（跨进程透传）。
	ctx = tracex.ContextFromCarrier(ctx, map[string]string{
		tracex.TraceParentKey: payload.TraceParent,
	})

	// 包 worker.process span：把整条异步处理（锁/CAS/ReAct/落库）挂进 HTTP 恢复出的 trace。
	ctx, span := otel.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/worker").Start(ctx, "worker.process")
	defer span.End()
	span.SetAttributes(
		attribute.String("msg_id", payload.MsgID),
		attribute.String("worker_id", c.workerID),
	)
	// 入口派生 logger 放在开 span 之后：日志的 trace_id/span_id 指向 worker.process 本身。
	logger := tracex.Logger(c.logger, ctx)
	logger.Info("worker: processing message",
		zap.String("msgID", payload.MsgID),
		zap.String("workerID", c.workerID))

	// Step 1: Redis NX lock — prevent duplicate execution
	hbKey := fmt.Sprintf("cg:hb:%s", payload.MsgID)
	acquired, err := c.lock.SetNX(ctx, hbKey, c.workerID, 30*time.Second)
	if err != nil || !acquired {
		logger.Warn("worker: could not acquire heartbeat lock", zap.String("msgID", payload.MsgID))
		_ = c.streamQ.Ack(ctx, msg.ID)
		return
	}

	// Step 2: MySQL CAS takeover — Pending → InProgress。
	// asyncRequestID 是雪花业务键（stream 载荷携带的值），
	// Takeover 用 async_request_id 做 CAS 匹配，而不是自增主键 id。
	asyncReq, err := c.asyncRepo.Takeover(ctx, payload.AsyncRequestID, c.workerID)
	if err != nil || asyncReq == nil {
		logger.Warn("worker: takeover failed (already taken)", zap.String("msgID", payload.MsgID))
		_ = c.lock.Del(ctx, hbKey) // Release lock
		_ = c.streamQ.Ack(ctx, msg.ID)
		return
	}

	// Step 3: Start heartbeat goroutine
	hbDone := make(chan struct{})
	go runHeartbeat(ctx, hbDone, c.lock, c.asyncRepo, payload.MsgID, asyncReq.ID, c.workerID, logger)
	defer close(hbDone)

	// Step 3.5 (Phase 4): 快捷路径（HIL resume / Retry / CanFastTask）。
	// 命中后由 Worker 直接处理回流，不跑管家 ReAct。
	// FastPath 判定通过 hooks 注入（决策归 app），worker 只负责执行命中后的分支。
	if fp := c.fastPathOr(); fp != nil {
		handled, fpErr := fp.CheckFastTask(ctx, &fastpath.FastTaskRequest{
			SessionID:   payload.SessionID,
			UserID:      payload.UserID,
			AgentID:     payload.AgentID,
			Query:       payload.Query,
			RequestType: payload.RequestType,
			Resume:      payload.Resume,
			MsgID:       payload.MsgID,
			OriginMsgID: payload.OriginMsgID,
		})
		if fpErr != nil {
			logger.Error("worker: fastpath error", zap.Error(fpErr))
		}
		if handled {
			if payload.Resume {
				// HIL resume：预置 checkpoint 后正常跑 ReAct（引擎从断点继续）。
				// cp==nil 时说明 checkpoint 已被另一连接恢复后清除（竞态）——
				// 写提示帧而非零帧静默，避免 resume 意图丢失。
				loader := c.checkpointLoader()
				var cp *react.Checkpoint
				if loader != nil {
					cp = loader(payload.SessionID)
				}
				if cp != nil {
					logger.Info("worker: HIL resume 恢复执行",
						zap.String("sessionID", payload.SessionID))
					c.runReActWithCheckpoint(ctx, payload, cp)
				} else {
					logger.Warn("worker: resume 命中但 checkpoint 已不存在（可能已恢复）",
						zap.String("sessionID", payload.SessionID))
					_ = c.chunkStore.Write(ctx, payload.MsgID,
						appkitrunner.FrameToJSON(appkitrunner.SSEFrame{Type: "error", Content: "恢复点已被消费，请重新发起请求"}), false)
					_ = c.chunkStore.Write(ctx, payload.MsgID, "", true)
				}
			} else {
				// CanFastTask/Retry：已投递子任务，轮询回流
				c.fastTaskPollAndFinish(ctx, payload)
			}
			c.asyncRepo.MarkTerminal(ctx, asyncReq.ID, model.AsyncStatusCompleted)
			_ = c.lock.Del(ctx, hbKey)
			_ = c.streamQ.Ack(ctx, msg.ID)
			return
		}
	}

	// Step 4: Run the ReAct business logic, writing output to ChunkStore
	runner := &appkitrunner.SupervisorRunner{}
	var planTaskRunID int64
	onFrame := func(frame appkitrunner.SSEFrame) {
		frameJSON := appkitrunner.FrameToJSON(frame)
		if err := c.chunkStore.Write(ctx, payload.MsgID, frameJSON, false); err != nil {
			logger.Error("worker: chunk write error", zap.Error(err))
		}
	}

	if c.taskSubmitter != nil {
		// Phase 3: 管家产出 new_agents_plan → 投递子任务

		// 流式恢复：每轮 LLM 输出实时写 Redis chunk（token 级累积，崩溃可重建）
		runner.EnableStreaming()
		thoughtHooks := c.thoughtHooksFor(payload, logger)

		// 业务装配（管家约束链策略 + 管家 prompt + HIL 注入）走 hooks。
		// UseManagerStrategy=true（app 在配置了真实管家 LLM 时置）→ 注入
		// ManagerStrategy + ManagerPrepare；否则回退 FakePlanStrategy 默认行为。
		// 是否走 RunWithOutput 由 taskSubmitter 是否接线决定（执行细节）。
		c.setupRunner(runner, func(cp *react.Checkpoint) {
			// Phase 4 HIL：管家 ReAct 产出 Interrupt 时，保存 checkpoint。
			// onInterrupt 回调拿到引擎存的完整 checkpoint（messages+step+query）。
			saver := c.checkpointSaver()
			if saver != nil {
				saver(payload.SessionID, cp)
				logger.Info("worker: HIL checkpoint 已保存",
					zap.String("sessionID", payload.SessionID),
					zap.Int("step", cp.Step))
			}
		})

		// Phase 4 HIL：query 命中触发词（app 注入的 ShouldEnableHIL）时，
		// 管家 ReAct 会产 Interrupt（FakeHILStrategy 判据），配合上面的
		// SetOnInterrupt 保存 checkpoint → 前端可点"继续"发起 resume。
		// thought 与 HIL 并存：用 thoughtHooks 替代 FakeHooks，避免 hooks 被覆盖。
		if c.hilEnabled(payload.Query) {
			hooks := thoughtHooks
			if hooks == nil {
				hooks = &appkitfake.FakeHooks{}
			}
			runner.SetProviders(nil, appkitfake.FakeHILStrategy{}, hooks)
		}

		_, runErr := runner.RunWithOutput(ctx, payload.Query, payload.MsgID, onFrame, func(plan *appkitrunner.PlanOutput) {
			run := &model.CGTaskRun{
				UserID:        payload.UserID,
				AgentID:       payload.AgentID,
				SessionID:     payload.SessionID,
				OriginalQuery: payload.Query,
				MsgID:         payload.MsgID,
				Status:        model.TaskRunStatusPending,
			}
			if err := run.SetPlanSteps(plan.ToModelPlan()); err != nil {
				logger.Error("worker: set plan_steps error", zap.Error(err))
				return
			}
			id, err := c.taskSubmitter.Submit(ctx, run)
			if err != nil {
				logger.Error("worker: submit task error", zap.Error(err))
				return
			}
			planTaskRunID = id

			// Phase 4 cron：requestType=cron → 登记 cg_cron_task（完整 agents 列表 + 推进索引 0）
			if payload.RequestType == "cron" && c.cronRepo != nil {
				cron := &model.CGCronTask{
					CronTaskID:       model.NextID(),
					SessionID:        payload.SessionID,
					MsgID:            payload.MsgID,
					PlanSteps:        run.PlanSteps, // 完整 agents JSON（执行器只吃第一个，Cron Poller 推进后续）
					CurrentStepIndex: 0,
					Status:           model.CronTaskStatusRunning,
				}
				if err := c.cronRepo.Insert(ctx, cron); err != nil {
					logger.Error("worker: cron task 登记失败", zap.Error(err))
				} else {
					logger.Info("worker: cron task registered",
						zap.Int64("cronTaskID", cron.CronTaskID),
						zap.String("sessionID", payload.SessionID))
				}
			}
		})
		if runErr != nil {
			logger.Error("worker: supervisor run error", zap.Error(runErr))
			c.asyncRepo.MarkTerminal(ctx, asyncReq.ID, model.AsyncStatusFailed)
			_ = c.lock.Del(ctx, hbKey)
			_ = c.streamQ.Ack(ctx, msg.ID)
			return
		}

		// 已投递子任务 → 同步轮询 task_step 回流进度（阻塞到终态），
		// 进度帧实时写 chunk → attach 拉 → 前端 SSE。全部完成后才写 EndTurn，
		// 保证前端连接在子任务执行期间保持打开、能收到 task-step/task-done 帧。
		if planTaskRunID != 0 && c.taskPoller != nil {
			// pollCtx 从带 span 的 ctx 派生：轮询 SQL span 挂进 worker.process trace，
			// 而不是 Background（否则长达 60s 的轮询阶段脱离 trace）。
			pollCtx, pollCancel := context.WithTimeout(ctx, 60*time.Second)
			c.taskPoller.PollUntilDone(pollCtx, planTaskRunID, payload.MsgID,
				func(f taskflow.TaskStepFrame) {
					frameJSON := taskflow.FrameToJSON(f)
					if err := c.chunkStore.Write(pollCtx, payload.MsgID, frameJSON, false); err != nil {
						logger.Error("worker: poll frame write error", zap.Error(err))
					}
				}, 500*time.Millisecond)
			pollCancel()

			// 子任务完成 → 写 EndTurn 让 attach 退出
			if err := c.chunkStore.Write(ctx, payload.MsgID, "", true); err != nil {
				logger.Error("worker: endturn write error", zap.Error(err))
			}

			// 落库兜底：flush 失败不标 Completed、不 ACK，交由 recovery 从 chunk 重建。
			if err := c.flushChatTextToDB(ctx, payload.MsgID); err != nil {
				if !errors.Is(err, ErrChunkExpired) {
					logger.Error("worker: flushChatTextToDB error — 保持 InProgress 等待 recovery 重建",
						zap.Error(err), zap.String("msgID", payload.MsgID))
					_ = c.lock.Del(ctx, hbKey)
					return
				}
				logger.Warn("worker: chunks expired, marking Failed", zap.String("msgID", payload.MsgID))
				_ = c.asyncRepo.MarkTerminal(ctx, asyncReq.ID, model.AsyncStatusFailed)
				_ = c.lock.Del(ctx, hbKey)
				_ = c.streamQ.Ack(ctx, msg.ID)
				return
			}
			c.asyncRepo.MarkTerminal(ctx, asyncReq.ID, model.AsyncStatusCompleted)
			_ = c.lock.Del(ctx, hbKey)
			_ = c.streamQ.Ack(ctx, msg.ID)
			logger.Info("worker: message completed with plan", zap.String("msgID", payload.MsgID))
			return
		}
	} else {
		// 无子任务投递：普通 ReAct。流式恢复：每轮 LLM 输出实时写 Redis chunk。
		runner.EnableStreaming()
		thoughtHooks := c.thoughtHooksFor(payload, logger)
		if thoughtHooks != nil {
			runner.SetProviders(nil, nil, thoughtHooks)
		}
		_, runErr := runner.Run(ctx, payload.Query, payload.MsgID, onFrame)
		if runErr != nil {
			logger.Error("worker: supervisor run error", zap.Error(runErr))
			c.asyncRepo.MarkTerminal(ctx, asyncReq.ID, model.AsyncStatusFailed)
			_ = c.lock.Del(ctx, hbKey)
			_ = c.streamQ.Ack(ctx, msg.ID)
			return
		}
	}

	// Step 5: Write EndTurn chunk
	if err := c.chunkStore.Write(ctx, payload.MsgID, "", true); err != nil {
		logger.Error("worker: endturn write error", zap.Error(err))
	}

	// Step 6: FlushChatTextToDB — assemble all chunks and write to message_record
	if err := c.flushChatTextToDB(ctx, payload.MsgID); err != nil {
		// 落库兜底：flush 失败（Redis 读失败 / chunk 已过期 / MySQL 写失败）时不标 Completed、
		// 不 ACK，保持 InProgress 且心跳随之停止 → 由 RecoveryScanner 扫到后从 Redis chunk 重建落库。
		// 重建成功→策略 A 翻 Completed；重建失败→Failed（避免无限重试死循环）。数据以 chunk 为准。
		// 例外：ErrChunkExpired 表示 chunk 已过期（TTL 1h），ReAct 产出已无从重建——
		// 用户连接早已关闭，只落空 chat_text + 标 Failed，避免留下"已完成但无内容"的脏记录。
		// 详见 docs/impl/async-flush-failure-recovery.md。
		if !errors.Is(err, ErrChunkExpired) {
			// 不 ACK：让 Redis Stream 消息留在 Pending（同一条消息在超时窗口内重新投递，
			// 但 NX 锁已删 + 状态 InProgress，新消费会因 Takeover 冲突而快速 ACK 跳过，
			// 不会重复执行）。真正的接管者是 recovery 对 cg_async_request 的重建+翻态。
			logger.Error("worker: flushChatTextToDB error — 保持 InProgress 等待 recovery 重建",
				zap.Error(err), zap.String("msgID", payload.MsgID))
			_ = c.lock.Del(ctx, hbKey)
			return
		}

		// chunk 过期：重建无输入，只能标记失败收口
		logger.Warn("worker: chunks expired, marking Failed",
			zap.String("msgID", payload.MsgID))
		_ = c.asyncRepo.MarkTerminal(ctx, asyncReq.ID, model.AsyncStatusFailed)
		_ = c.lock.Del(ctx, hbKey)
		_ = c.streamQ.Ack(ctx, msg.ID)
		return
	}

	// Step 7: Mark terminal + release lock + ACK
	c.asyncRepo.MarkTerminal(ctx, asyncReq.ID, model.AsyncStatusCompleted)
	_ = c.lock.Del(ctx, hbKey)
	_ = c.streamQ.Ack(ctx, msg.ID)

	logger.Info("worker: message completed", zap.String("msgID", payload.MsgID))
}

// thoughtHooksFor 构建落 thought 表的 hooks（OnAfterLLM 每轮落一条）。
// hooks 未装配 ThoughtRepo 时返回 nil（跳过 thought 落库）。
func (c *Consumer) thoughtHooksFor(payload conversationPayload, logger *zap.Logger) react.Hooks {
	if c.hooks == nil || c.hooks.ThoughtRepo == nil {
		return nil
	}
	return NewThoughtHooks(c.hooks.ThoughtRepo, &payload, "", logger)
}

// runReActWithCheckpoint 用 HIL checkpoint 预置 AgentContext 跑 ReAct。
// 引擎 resume 支持：Runtime.Messages/Step 预置后从断点继续，且必须置 Interrupted=true
// 让 HIL strategy 识别这是"恢复轮次"（直接 Terminal 而非再次 Interrupt）。
func (c *Consumer) runReActWithCheckpoint(ctx context.Context, payload conversationPayload, cp *react.Checkpoint) {
	runner := &appkitrunner.SupervisorRunner{}
	onFrame := func(frame appkitrunner.SSEFrame) {
		frameJSON := appkitrunner.FrameToJSON(frame)
		if err := c.chunkStore.Write(ctx, payload.MsgID, frameJSON, false); err != nil {
			c.logger.Error("worker: chunk write error", zap.Error(err))
		}
	}
	// 流式恢复：每轮 LLM 输出实时写 Redis chunk（token 级累积，崩溃可重建）
	runner.EnableStreaming()
	thoughtHooks := c.thoughtHooksFor(payload, c.logger)
	// 注入 HIL strategy（resume 时直接 Terminal）。
	// thought 与 HIL 并存：用 thoughtHooks 替代 FakeHooks，避免 hooks 被覆盖。
	hooks := thoughtHooks
	if hooks == nil {
		hooks = &appkitfake.FakeHooks{}
	}
	runner.SetProviders(&appkitfake.FakeHILLLM{}, appkitfake.FakeHILStrategy{}, hooks)
	// RunResume 预置 checkpoint 状态（messages/step/Interrupted）后跑 ReAct
	_, _ = runner.RunResume(ctx, payload.Query, payload.MsgID, onFrame, cp)

	// 写 EndTurn
	_ = c.chunkStore.Write(ctx, payload.MsgID, "", true)
	if err := c.flushChatTextToDB(ctx, payload.MsgID); err != nil {
		c.logger.Error("worker: flushChatTextToDB error", zap.Error(err))
	}
	// resume 完成 → 清除 checkpoint（避免下次误判）
	if clearer := c.checkpointClearer(); clearer != nil {
		clearer(payload.SessionID)
	}
}

// fastTaskPollAndFinish 快路径命中（已投递子任务）后：轮询 task_step 回流 + 写 EndTurn。
func (c *Consumer) fastTaskPollAndFinish(ctx context.Context, payload conversationPayload) {
	if c.taskRunRepo == nil || c.taskPoller == nil {
		c.logger.Warn("worker: fasttask 缺 taskRunRepo/taskPoller，仅写 EndTurn",
			zap.String("msgID", payload.MsgID))
		_ = c.chunkStore.Write(ctx, payload.MsgID, "", true)
		return
	}
	// 从 DB 查该 msgID 关联的最新 task_run
	run, err := c.taskRunRepo.GetByMsgID(ctx, payload.MsgID)
	if err != nil || run == nil {
		c.logger.Warn("worker: fasttask 未找到关联 task_run",
			zap.String("msgID", payload.MsgID))
		_ = c.chunkStore.Write(ctx, payload.MsgID, "", true)
		return
	}
	// 轮询 SQL/Redis span 挂进 worker.process trace（ctx 带 span，不用 Background）。
	pollCtx, pollCancel := context.WithTimeout(ctx, 60*time.Second)
	defer pollCancel()
	c.taskPoller.PollUntilDone(pollCtx, run.TaskRunID, payload.MsgID,
		func(f taskflow.TaskStepFrame) {
			frameJSON := taskflow.FrameToJSON(f)
			if err := c.chunkStore.Write(pollCtx, payload.MsgID, frameJSON, false); err != nil {
				c.logger.Error("worker: poll frame write error", zap.Error(err))
			}
		}, 500*time.Millisecond)
	// 子任务完成 → 发 message-complete 帧（前端据此释放 streaming 状态），再写 EndTurn
	_ = c.chunkStore.Write(ctx, payload.MsgID,
		appkitrunner.FrameToJSON(appkitrunner.SSEFrame{Type: "message-complete", MsgID: payload.MsgID}), false)
	_ = c.chunkStore.Write(ctx, payload.MsgID, "", true)
	if err := c.flushChatTextToDB(ctx, payload.MsgID); err != nil {
		c.logger.Error("worker: flushChatTextToDB error", zap.Error(err))
	}
}

// flushChatTextToDB reads all chunks from ChunkStore and writes them into
// the message_record's chat_text via ReadModifyWriteInTx.
func (c *Consumer) flushChatTextToDB(ctx context.Context, msgID string) error {
	return RebuildChatTextFromChunks(ctx, c.msgRepo, c.chunkStore, msgID)
}

// ErrChunkExpired 表示 Redis chunk 已过期（TTL 1h），无法从 chunk 重建 chat_text。
// 此时终端用户连接已关闭，落库只能保持缺失；调用方需自行决定终态语义。
var ErrChunkExpired = errors.New("chunks expired: no EndTurn found before chunk TTL")

// RebuildChatTextFromChunks 从 ChunkStore 读全量 chunk 组装 chat_text 写回 message_record。
// 幂等：重复调用只会重建出相同内容，可安全地被 worker 与 recovery 复用。
// 返回 ErrChunkExpired 时表示 chunk 已过期（读到 0 帧且无 EndTurn），
// 此时重建无输入，调用方应降级（仅翻终态/标记失败），不能据此声称已重建。
func RebuildChatTextFromChunks(ctx context.Context, msgRepo repository.MessageRecordRepository,
	chunkStore infra.ChunkStore, msgID string) error {
	// Read all chunks from the store
	var cursor int64
	var allChunks []string

	for {
		chunks, newCursor, _, err := chunkStore.After(ctx, msgID, cursor)
		if err != nil {
			return fmt.Errorf("flushChatTextToDB: read chunks: %w", err)
		}
		for _, ch := range chunks {
			if ch.EndTurn {
				goto flush
			}
			allChunks = append(allChunks, ch.Payload)
		}
		if newCursor == cursor {
			return ErrChunkExpired
		}
		cursor = newCursor
	}

flush:
	return msgRepo.ReadModifyWriteInTx(ctx, msgID, func(rec *model.CGMessageRecord) (map[string]any, error) {
		ct := &model.ChatText{}
		if err := json.Unmarshal([]byte(rec.ChatText), ct); err != nil {
			return nil, fmt.Errorf("flushChatTextToDB: unmarshal chat_text: %w", err)
		}

		// Build chat response items from chunks
		items := make([]model.ChatResponseItem, 0, len(allChunks))
		for i, ch := range allChunks {
			items = append(items, model.ChatResponseItem{
				MsgID:   msgID,
				SendID:  int64(i + 1),
				Message: ch,
				EndTurn: false,
			})
		}
		// Mark last item as endTurn
		if len(items) > 0 {
			items[len(items)-1].EndTurn = true
		}
		ct.ChatResponse = items

		ctJSON, err := json.Marshal(ct)
		if err != nil {
			return nil, fmt.Errorf("flushChatTextToDB: marshal chat_text: %w", err)
		}

		return map[string]any{
			"chat_text": string(ctJSON),
			"status":    model.MessageStatusNormal,
		}, nil
	})
}
