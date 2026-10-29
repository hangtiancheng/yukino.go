package taskflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/executor"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// Consumer is the subtask consumer (corresponding to the reference system's executor/consumer.go).
//
// Reliability is lighter than the conversation pipeline (③) — subtasks are pure background tasks with no connection waiting:
//   - MsgID lock dedup (only one worker processes a given message)
//   - CAS claim of cg_task_run (Pending→Running)
//   - Timeout/exception without ACK → the stream message stays pending → the task stream's recovery redispatches it
//
// No heartbeat, no zombie scan (the conversation pipeline's RecoveryScanner only scans cg_async_request, not task_run).
type Consumer struct {
	streamQ     infra.StreamQueue
	taskRunRepo repository.TaskRunRepository
	executor    *subtaskexecutor.TaskExecutor // executor (Step 3.4); when nil only claim validation runs
	executorID  string                        // unique identifier of this consumer (the owner recorded by the CAS claim)
	logger      *zap.Logger
}

// ConsumerConfig holds the Consumer's dependencies.
type ConsumerConfig struct {
	StreamQ     infra.StreamQueue
	TaskRunRepo repository.TaskRunRepository
	Executor    *subtaskexecutor.TaskExecutor
	ExecutorID  string
	Logger      *zap.Logger
}

// NewConsumer creates the subtask consumer.
func NewConsumer(cfg ConsumerConfig) *Consumer {
	if cfg.ExecutorID == "" {
		cfg.ExecutorID = "executor-1"
	}
	return &Consumer{
		streamQ:     cfg.StreamQ,
		taskRunRepo: cfg.TaskRunRepo,
		executor:    cfg.Executor,
		executorID:  cfg.ExecutorID,
		logger:      cfg.Logger,
	}
}

// Start runs the consumption loop. It blocks until ctx is canceled.
func (c *Consumer) Start(ctx context.Context) {
	c.logger.Info("task consumer started", zap.String("executorID", c.executorID))
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("task consumer stopped")
			return
		default:
		}

		msg, err := c.streamQ.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Error("task consumer receive error", zap.Error(err))
			continue
		}
		if msg == nil {
			continue
		}

		// Process concurrently; slow tasks do not block polling
		go c.handleTask(ctx, msg)
	}
}

// handleTask processes one subtask message.
func (c *Consumer) handleTask(ctx context.Context, msg *infra.StreamMessage) {
	var payload subtaskexecutor.TaskPayload
	if err := json.Unmarshal(msg.Body, &payload); err != nil {
		c.logger.Error("task consumer: unmarshal payload error", zap.Error(err))
		_ = c.streamQ.Ack(ctx, msg.ID) // Poison message, ack to avoid blocking
		return
	}

	// Restore the trace from the stream (cross-process propagation).
	ctx = tracex.ContextFromCarrier(ctx, map[string]string{
		tracex.TraceParentKey: payload.TraceParent,
	})

	// Wrap the task.process span: hang subtask processing (claim/execute/persist) under the trace restored from HTTP.
	ctx, span := otel.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/executor").Start(ctx, "task.process")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("task_run_id", payload.TaskRunID),
		attribute.String("msg_id", payload.MsgID),
	)
	// Derive the entry logger after starting the span: log trace_id/span_id then point at task.process itself.
	logger := tracex.Logger(c.logger, ctx)
	logger.Info("task consumer: processing message",
		zap.Int64("taskRunID", payload.TaskRunID),
		zap.String("msgID", payload.MsgID),
		zap.String("executorID", c.executorID))

	// Step 1: replica-lag retry — the Submitter Pushes right after INSERT; avoid reading nil.
	run, err := c.getRunWithRetry(ctx, payload.TaskRunID)
	if err != nil {
		logger.Error("task consumer: get task_run error",
			zap.Int64("taskRunID", payload.TaskRunID), zap.Error(err))
		return // no ACK; wait for redispatch
	}

	// Step 2: state machine + CAS claim
	acquired, skip := c.preempt(ctx, run)
	if skip {
		_ = c.streamQ.Ack(ctx, msg.ID)
		return
	}
	if !acquired {
		logger.Warn("task consumer: CAS claim failed (an executor already holds it)",
			zap.Int64("taskRunID", payload.TaskRunID))
		_ = c.streamQ.Ack(ctx, msg.ID)
		return
	}

	// Step 3: hand off to the TaskExecutor (including NeedWait branching + result persistence)
	if c.executor == nil {
		// No executor wired: only validate the claim and terminate directly (used by the Step 3.3 unit tests)
		_ = c.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFinished, "")
		_ = c.streamQ.Ack(ctx, msg.ID)
		return
	}

	shouldAck := c.executor.Execute(ctx, run)
	if shouldAck {
		_ = c.streamQ.Ack(ctx, msg.ID)
	} else {
		logger.Warn("task consumer: execution incomplete (needs redispatch), not ACKing",
			zap.Int64("taskRunID", payload.TaskRunID))
	}
}

// getRunWithRetry retries up to 3 times × 200ms to avoid reading nil (the Submitter dispatches right after inserting).
func (c *Consumer) getRunWithRetry(ctx context.Context, taskRunID int64) (*model.CGTaskRun, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		run, err := c.taskRunRepo.GetByTaskRunID(ctx, taskRunID)
		if err == nil {
			return run, nil
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("get task_run after 3 retries: %w", lastErr)
}

// preempt CAS-claims the task_run. Returns (claimed, shouldSkip).
//
//	switch status:
//	  Pending          → proceed with the claim (Pending→Running)
//	  Finished/Failed  → idempotent return, skip
//	  Running          → zombie check: an overly old update counts as a crash; reset to Pending and re-claim;
//	                     otherwise assume an active executor and skip
func (c *Consumer) preempt(ctx context.Context, run *model.CGTaskRun) (acquired bool, skip bool) {
	switch run.Status {
	case model.TaskRunStatusFinished, model.TaskRunStatusFailed:
		return false, true // idempotent return

	case model.TaskRunStatusRunning:
		// Zombie criterion: UpdateTime older than workerTimeout (5min here) counts as a crashed executor
		workerTimeout := 5 * time.Minute
		if time.Since(time.UnixMilli(run.UpdateTime)) < workerTimeout {
			return false, true // an active executor exists; skip
		}
		// Zombie: reset to Pending and re-claim
		ok, err := c.taskRunRepo.UpdateCAS(ctx, run.TaskRunID, model.TaskRunStatusRunning, map[string]any{
			"status": model.TaskRunStatusPending,
		})
		if err != nil || !ok {
			return false, true
		}
		// After falling back to Pending, continue below (the Pending branch)
		run.Status = model.TaskRunStatusPending
	}

	// Pending → Running (CAS claim)
	ok, err := c.taskRunRepo.UpdateCAS(ctx, run.TaskRunID, model.TaskRunStatusPending, map[string]any{
		"status":      model.TaskRunStatusRunning,
		"consumer_ip": c.executorID,
		"start_time":  time.Now().UnixMilli(),
	})
	if err != nil || !ok {
		return false, false
	}
	return true, false
}
