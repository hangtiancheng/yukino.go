package subtaskexecutor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	"go.uber.org/zap"
)

// TaskPayload is the message body of the subtask stream.
// It carries only locating information (task_run_id business key + msg_id dedup key); the execution context is read from the DB.
// Dispatched by subtaskexecutor.Submitter and consumed by taskflow.Consumer, hence exported across packages.
type TaskPayload struct {
	TaskRunID int64  `json:"task_run_id"`
	MsgID     string `json:"msg_id"`
	UserID    int64  `json:"user_id"`
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	Query     string `json:"query"`
	// TraceParent is the W3C traceparent, propagating the trace across processes.
	TraceParent string `json:"traceparent,omitempty"`
}

// Submitter is the subtask launcher (corresponding to the reference system's AsyncSubmitter).
//
// Two reliability essentials (see mini-async-task-impl.md §4-a):
//  1. Create task_run(Pending) before dispatching — dispatch may fail, but "pending execution" is already persisted
//  2. Dispatch failure does not block the main flow — mark Failed and still return the id; the manager does not fall over because one subtask dispatch failed
type Submitter struct {
	taskRunRepo repository.TaskRunRepository
	streamQ     infra.StreamQueue
	logger      *zap.Logger
}

// SubmitterConfig holds the Submitter's dependencies.
type SubmitterConfig struct {
	TaskRunRepo repository.TaskRunRepository
	StreamQ     infra.StreamQueue
	Logger      *zap.Logger
}

// NewSubmitter creates a Submitter.
func NewSubmitter(cfg SubmitterConfig) *Submitter {
	return &Submitter{
		taskRunRepo: cfg.TaskRunRepo,
		streamQ:     cfg.StreamQ,
		logger:      cfg.Logger,
	}
}

// Submit creates task_run(Pending) first, then dispatches.
// Returns taskRunID: as long as task_run is created it returns id + nil (dispatch failure does not block; it marks Failed).
// Only when creating task_run itself fails does it return an error.
func (s *Submitter) Submit(ctx context.Context, run *model.CGTaskRun) (int64, error) {
	// 1. Persist first (the first reliability guard)
	if run.TaskRunID == 0 {
		run.TaskRunID = model.NextID()
	}
	if err := s.taskRunRepo.Insert(ctx, run); err != nil {
		return 0, fmt.Errorf("submitter: create task_run: %w", err)
	}

	// 2. Then dispatch (the real version retries 3 times; simplified here to one attempt, marking Failed on failure)
	guid := fmt.Sprintf("task-%d", run.TaskRunID)
	body, _ := json.Marshal(TaskPayload{
		TaskRunID:   run.TaskRunID,
		MsgID:       run.MsgID, // the real messageID (previously a "task-{id}" guid)
		UserID:      run.UserID,
		SessionID:   run.SessionID,
		AgentID:     run.AgentID,
		Query:       run.OriginalQuery,
		TraceParent: tracex.InjectCarrier(ctx)["traceparent"],
	})
	if err := s.streamQ.Push(ctx, guid, body); err != nil {
		// Dispatch failure does not block the main flow, but the task_run must be marked Failed (otherwise it stays Pending forever)
		s.logger.Error("submitter: dispatch failed, marking task_run Failed",
			zap.Int64("taskRunID", run.TaskRunID),
			zap.Error(err))
		_ = s.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFailed, "submitter: dispatch failed: "+err.Error())
		return run.TaskRunID, nil
	}

	return run.TaskRunID, nil
}
