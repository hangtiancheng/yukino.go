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
// Phase 3 extension: when the manager produces new_agents_plan, subtasks are dispatched via the Submitter,
// and a Poller polls cg_task_step, pushing progress as chunks back to the frontend.
type Consumer struct {
	streamQ    infra.StreamQueue                  // message queue: XREADGROUP/XACK
	chunkStore infra.ChunkStore                   // chunk relay store: Write/After
	lock       infra.Locker                       // heartbeat lock (Redis or in-process implementation)
	asyncRepo  repository.AsyncRequestRepository  // cg_async_request DAO
	msgRepo    repository.MessageRecordRepository // cg_message_record DAO
	workerID   string                             // unique identifier of this worker
	logger     *zap.Logger

	// Phase 3: subtask chain (the plan branch is skipped when nil)
	taskSubmitter *subtaskexecutor.Submitter
	taskPoller    *taskflow.Poller

	// Phase 4: cron registration (skipped when nil)
	cronRepo repository.CronTaskRepository
	// Phase 4: locate task_run for fast-task backflow (GetByMsgID)
	taskRunRepo repository.TaskRunRepository

	// Business decision callback (injected by the app wiring). When nil it degrades to a consumption shell without business wiring.
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

	// Phase 3: subtask chain (the plan branch is skipped when nil).
	TaskSubmitter *subtaskexecutor.Submitter
	TaskPoller    *taskflow.Poller

	// Phase 4: cron registration (skipped when nil).
	CronRepo repository.CronTaskRepository
	// Phase 4: locate task_run for fast-task backflow (GetByMsgID).
	TaskRunRepo repository.TaskRunRepository
	// Phase 4: business decision callback (injected by the app wiring; when nil it degrades to a consumption shell without business wiring).
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

	// Phase 4: fast-path parameters (passed through by con_reader in the stream body)
	RequestType string `json:"requestType"` // "" / "retry" / "cron"
	Resume      bool   `json:"resume"`      // HIL breakpoint resume
	// OriginMsgID is the previous-step message ID echoed after card confirmation (fastpath uses it to trace the preceding script).
	OriginMsgID string `json:"originMsgId"`

	// TraceParent is the W3C traceparent, propagating the trace across processes.
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

	// Restore the trace from the stream (cross-process propagation).
	ctx = tracex.ContextFromCarrier(ctx, map[string]string{
		tracex.TraceParentKey: payload.TraceParent,
	})

	// Wrap the worker.process span: hang the whole async processing (lock/CAS/ReAct/persistence) under the trace restored from HTTP.
	ctx, span := otel.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/worker").Start(ctx, "worker.process")
	defer span.End()
	span.SetAttributes(
		attribute.String("msg_id", payload.MsgID),
		attribute.String("worker_id", c.workerID),
	)
	// Derive the entry logger after starting the span: log trace_id/span_id then point at worker.process itself.
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

	// Step 2: MySQL CAS takeover — Pending → InProgress.
	// asyncRequestID is the snowflake business key (the value carried in the stream payload);
	// Takeover CAS-matches on async_request_id, not the auto-increment primary key id.
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

	// Step 3.5 (Phase 4): fast path (HIL resume / Retry / CanFastTask).
	// On a hit, the Worker handles the backflow directly without running the manager ReAct.
	// The FastPath decision is injected via hooks (decisions belong to the app); the worker only executes the branch after a hit.
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
				// HIL resume: pre-load the checkpoint and run ReAct normally (the engine continues from the breakpoint).
				// cp==nil means the checkpoint was already consumed and cleared by another connection (race) —
				// write a hint frame instead of silent zero frames, so the resume intent is not lost.
				loader := c.checkpointLoader()
				var cp *react.Checkpoint
				if loader != nil {
					cp = loader(payload.SessionID)
				}
				if cp != nil {
					logger.Info("worker: HIL resume execution restored",
						zap.String("sessionID", payload.SessionID))
					c.runReActWithCheckpoint(ctx, payload, cp)
				} else {
					logger.Warn("worker: resume hit but the checkpoint no longer exists (possibly already resumed)",
						zap.String("sessionID", payload.SessionID))
					_ = c.chunkStore.Write(ctx, payload.MsgID,
						appkitrunner.FrameToJSON(appkitrunner.SSEFrame{Type: "error", Content: "the resume point has been consumed; please send a new request"}), false)
					_ = c.chunkStore.Write(ctx, payload.MsgID, "", true)
				}
			} else {
				// CanFastTask/Retry: subtasks already dispatched; poll the backflow
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
		// Phase 3: the manager produced new_agents_plan → dispatch subtasks

		// Streaming recovery: each LLM round writes Redis chunks in realtime (token-level accumulation, rebuildable after a crash)
		runner.EnableStreaming()
		thoughtHooks := c.thoughtHooksFor(payload, logger)

		// Business assembly (manager constraint-chain strategy + manager prompt + HIL injection) goes through hooks.
		// UseManagerStrategy=true (set by the app when a real manager LLM is configured) → inject
		// ManagerStrategy + ManagerPrepare; otherwise fall back to FakePlanStrategy default behavior.
		// Whether RunWithOutput is used depends on taskSubmitter being wired (an execution detail).
		c.setupRunner(runner, func(cp *react.Checkpoint) {
			// Phase 4 HIL: when the manager ReAct produces Interrupt, save the checkpoint.
			// The onInterrupt callback receives the full checkpoint stored by the engine (messages+step+query).
			saver := c.checkpointSaver()
			if saver != nil {
				saver(payload.SessionID, cp)
				logger.Info("worker: HIL checkpoint saved",
					zap.String("sessionID", payload.SessionID),
					zap.Int("step", cp.Step))
			}
		})

		// Phase 4 HIL: when the query hits the trigger word (the app-injected ShouldEnableHIL),
		// the manager ReAct produces Interrupt (the FakeHILStrategy criterion); combined with
		// SetOnInterrupt above it saves the checkpoint → the frontend can click "Continue" to resume.
		// thought and HIL coexist: thoughtHooks replaces FakeHooks so hooks are not overwritten.
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

			// Phase 4 cron: requestType=cron → register cg_cron_task (full agents list + advance index 0)
			if payload.RequestType == "cron" && c.cronRepo != nil {
				cron := &model.CGCronTask{
					CronTaskID:       model.NextID(),
					SessionID:        payload.SessionID,
					MsgID:            payload.MsgID,
					PlanSteps:        run.PlanSteps, // full agents JSON (the executor consumes only the first; the Cron Poller advances the rest)
					CurrentStepIndex: 0,
					Status:           model.CronTaskStatusRunning,
				}
				if err := c.cronRepo.Insert(ctx, cron); err != nil {
					logger.Error("worker: failed to register cron task", zap.Error(err))
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

		// Subtasks dispatched → synchronously poll task_step for progress backflow (blocking until terminal);
		// progress frames are written to chunks in realtime → pulled by attach → frontend SSE. EndTurn is written only after everything completes,
		// guaranteeing the frontend connection stays open during subtask execution and receives task-step/task-done frames.
		if planTaskRunID != 0 && c.taskPoller != nil {
			// pollCtx derives from the span-carrying ctx: polling SQL spans hang under the worker.process trace
			// instead of Background (otherwise the up-to-60s polling phase would leave the trace).
			pollCtx, pollCancel := context.WithTimeout(ctx, 60*time.Second)
			c.taskPoller.PollUntilDone(pollCtx, planTaskRunID, payload.MsgID,
				func(f taskflow.TaskStepFrame) {
					frameJSON := taskflow.FrameToJSON(f)
					if err := c.chunkStore.Write(pollCtx, payload.MsgID, frameJSON, false); err != nil {
						logger.Error("worker: poll frame write error", zap.Error(err))
					}
				}, 500*time.Millisecond)
			pollCancel()

			// Subtasks complete → write EndTurn so attach exits
			if err := c.chunkStore.Write(ctx, payload.MsgID, "", true); err != nil {
				logger.Error("worker: endturn write error", zap.Error(err))
			}

			// Persistence fallback: on flush failure do not mark Completed and do not ACK; recovery rebuilds from chunks.
			if err := c.flushChatTextToDB(ctx, payload.MsgID); err != nil {
				if !errors.Is(err, ErrChunkExpired) {
					logger.Error("worker: flushChatTextToDB error — keeping InProgress for recovery rebuild",
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
		// No subtasks dispatched: plain ReAct. Streaming recovery: each LLM round writes Redis chunks in realtime.
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
		// Persistence fallback: on flush failure (Redis read failure / expired chunks / MySQL write failure) do not mark Completed
		// and do not ACK; keep InProgress and let the heartbeat stop → RecoveryScanner picks it up and rebuilds the persistence from Redis chunks.
		// Rebuild success → strategy A flips to Completed; rebuild failure → Failed (avoiding an infinite retry loop). Chunks are the source of truth.
		// Exception: ErrChunkExpired means the chunks expired (TTL 1h) and the ReAct output can no longer be rebuilt —
		// the user connection closed long ago, so persist an empty chat_text + mark Failed, avoiding a dirty "completed but empty" record.
		if !errors.Is(err, ErrChunkExpired) {
			// Do not ACK: leave the Redis Stream message Pending (the same message is redelivered within the timeout window,
			// but with the NX lock deleted + status InProgress, the new consumer quickly ACK-skips due to the Takeover conflict,
			// so there is no duplicate execution). The real takeover is recovery rebuilding cg_async_request and flipping its state.
			logger.Error("worker: flushChatTextToDB error — keeping InProgress for recovery rebuild",
				zap.Error(err), zap.String("msgID", payload.MsgID))
			_ = c.lock.Del(ctx, hbKey)
			return
		}

		// Chunks expired: no input for rebuilding; the only option is to mark it failed and close out
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

// thoughtHooksFor builds the hooks that persist to the thought table (OnAfterLLM writes one row per round).
// Returns nil when hooks have no ThoughtRepo wired (skipping thought persistence).
func (c *Consumer) thoughtHooksFor(payload conversationPayload, logger *zap.Logger) react.Hooks {
	if c.hooks == nil || c.hooks.ThoughtRepo == nil {
		return nil
	}
	return NewThoughtHooks(c.hooks.ThoughtRepo, &payload, "", logger)
}

// runReActWithCheckpoint pre-loads the AgentContext from the HIL checkpoint and runs ReAct.
// Engine resume support: with Runtime.Messages/Step pre-set it continues from the breakpoint, and Interrupted=true must be set
// so the HIL strategy recognizes this as a "resume round" (Terminal directly instead of Interrupting again).
func (c *Consumer) runReActWithCheckpoint(ctx context.Context, payload conversationPayload, cp *react.Checkpoint) {
	runner := &appkitrunner.SupervisorRunner{}
	onFrame := func(frame appkitrunner.SSEFrame) {
		frameJSON := appkitrunner.FrameToJSON(frame)
		if err := c.chunkStore.Write(ctx, payload.MsgID, frameJSON, false); err != nil {
			c.logger.Error("worker: chunk write error", zap.Error(err))
		}
	}
	// Streaming recovery: each LLM round writes Redis chunks in realtime (token-level accumulation, rebuildable after a crash)
	runner.EnableStreaming()
	thoughtHooks := c.thoughtHooksFor(payload, c.logger)
	// Inject the HIL strategy (Terminal directly on resume).
	// thought and HIL coexist: thoughtHooks replaces FakeHooks so hooks are not overwritten.
	hooks := thoughtHooks
	if hooks == nil {
		hooks = &appkitfake.FakeHooks{}
	}
	runner.SetProviders(&appkitfake.FakeHILLLM{}, appkitfake.FakeHILStrategy{}, hooks)
	// RunResume pre-loads the checkpoint state (messages/step/Interrupted) and runs ReAct
	_, _ = runner.RunResume(ctx, payload.Query, payload.MsgID, onFrame, cp)

	// Write EndTurn
	_ = c.chunkStore.Write(ctx, payload.MsgID, "", true)
	if err := c.flushChatTextToDB(ctx, payload.MsgID); err != nil {
		c.logger.Error("worker: flushChatTextToDB error", zap.Error(err))
	}
	// Resume complete → clear the checkpoint (avoiding a false hit next time)
	if clearer := c.checkpointClearer(); clearer != nil {
		clearer(payload.SessionID)
	}
}

// fastTaskPollAndFinish runs after a fast-path hit (subtasks already dispatched): poll task_step backflow + write EndTurn.
func (c *Consumer) fastTaskPollAndFinish(ctx context.Context, payload conversationPayload) {
	if c.taskRunRepo == nil || c.taskPoller == nil {
		c.logger.Warn("worker: fasttask missing taskRunRepo/taskPoller, writing EndTurn only",
			zap.String("msgID", payload.MsgID))
		_ = c.chunkStore.Write(ctx, payload.MsgID, "", true)
		return
	}
	// Query the DB for the latest task_run associated with this msgID
	run, err := c.taskRunRepo.GetByMsgID(ctx, payload.MsgID)
	if err != nil || run == nil {
		c.logger.Warn("worker: fasttask found no associated task_run",
			zap.String("msgID", payload.MsgID))
		_ = c.chunkStore.Write(ctx, payload.MsgID, "", true)
		return
	}
	// Polling SQL/Redis spans hang under the worker.process trace (ctx carries the span; Background is not used).
	pollCtx, pollCancel := context.WithTimeout(ctx, 60*time.Second)
	defer pollCancel()
	c.taskPoller.PollUntilDone(pollCtx, run.TaskRunID, payload.MsgID,
		func(f taskflow.TaskStepFrame) {
			frameJSON := taskflow.FrameToJSON(f)
			if err := c.chunkStore.Write(pollCtx, payload.MsgID, frameJSON, false); err != nil {
				c.logger.Error("worker: poll frame write error", zap.Error(err))
			}
		}, 500*time.Millisecond)
	// Subtasks complete → send the message-complete frame (the frontend releases its streaming state on it), then write EndTurn
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

// ErrChunkExpired indicates the Redis chunks expired (TTL 1h) and chat_text can no longer be rebuilt from them.
// By then the end-user connection has closed and persistence can only remain missing; the caller decides the terminal semantics.
var ErrChunkExpired = errors.New("chunks expired: no EndTurn found before chunk TTL")

// RebuildChatTextFromChunks reads all chunks from the ChunkStore, assembles chat_text, and writes it back to message_record.
// Idempotent: repeated calls rebuild the same content, safe for reuse by both worker and recovery.
// Returning ErrChunkExpired means the chunks expired (0 frames read and no EndTurn);
// with no input to rebuild, the caller should degrade (only flip the terminal state / mark failed) and must not claim a rebuild happened.
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
