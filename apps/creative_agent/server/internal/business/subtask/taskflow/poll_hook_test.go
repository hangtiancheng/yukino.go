package taskflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/logger"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/sse"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newExecutorDB creates an in-memory sqlite DB (migrating the task_run/task_step tables).
func newExecutorDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGTaskRun{}, &model.CGTaskStep{}))
	sqlDB, err := db.DB()
	testx.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	return db
}

// TestPoller_StreamsIncrementalSteps verifies that poll pushes only increments + task-done at terminal state.
func TestPoller_StreamsIncrementalSteps(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)

	// Create task_run + two steps
	run := &model.CGTaskRun{
		TaskRunID:     model.NextID(),
		UserID:        1,
		AgentID:       "gold_scriptwriter",
		SessionID:     "s1",
		OriginalQuery: "q",
		Status:        model.TaskRunStatusPending,
	}
	testx.NoError(t, taskRunRepo.Insert(ctx, run))

	step1 := &model.CGTaskStep{
		TaskStepID: model.NextID(),
		TaskRunID:  run.TaskRunID,
		UserID:     1,
		AgentID:    "gold_scriptwriter",
		SessionID:  "s1",
		StepName:   "gold_scriptwriter",
		Status:     model.TaskStepStatusRunning,
		StepOutput: `{"final_product":"正在写脚本..."}`,
	}
	testx.NoError(t, stepRepo.Insert(ctx, step1))

	poller := taskflow.NewPoller(taskflow.PollerConfig{
		StepRepo:    stepRepo,
		TaskRunRepo: taskRunRepo,
		Logger:      zlog,
	})

	var frames []taskflow.TaskStepFrame
	onFrame := func(f taskflow.TaskStepFrame) { frames = append(frames, f) }

	// Round 1: task_run still Pending → only task-step is pushed, no task-done
	done := poller.PollOnce(ctx, run.TaskRunID, "msg-1", onFrame)
	testx.False(t, done, "task_run not terminal; should not stop")
	testx.Len(t, frames, 1)
	testx.Equal(t, sse.EventTaskStep, frames[0].Type)

	// Advance: step1 → Finished + write step_output; task_run → Finished
	testx.NoError(t, stepRepo.UpdateByTaskStepID(ctx, step1.TaskStepID, map[string]any{
		"status":      model.TaskStepStatusFinished,
		"step_output": `{"final_product":"海报已交付"}`,
	}))
	testx.NoError(t, taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFinished, ""))

	// Round 2: run terminal → push task-done (carrying the newest Finished top-level step's output)
	frames = nil
	done = poller.PollOnce(ctx, run.TaskRunID, "msg-1", onFrame)
	testx.True(t, done, "polling should stop after task_run Finished")
	testx.Len(t, frames, 1, "the second round should push one run-terminal task-done")
	testx.Equal(t, sse.EventTaskDone, frames[0].Type)
	testx.Contains(t, frames[0].Output, "海报已交付", "task-done should carry the delivered product")
	testx.Equal(t, step1.TaskStepID, frames[0].TaskStepID)
}

// TestPoller_NeedWaitYes_StopsWithoutDone verifies NeedWait=Yes:
// when task_run is set to Waiting, polling stops but no task-done is pushed (terminal state not reached; awaiting the pipeline callback).
func TestPoller_NeedWaitYes_StopsWithoutDone(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)

	run := &model.CGTaskRun{
		TaskRunID:     model.NextID(),
		UserID:        1,
		AgentID:       "video_agent",
		SessionID:     "s1",
		OriginalQuery: "q",
		Status:        model.TaskRunStatusPending,
	}
	testx.NoError(t, taskRunRepo.Insert(ctx, run))

	agentStep := &model.CGTaskStep{
		TaskStepID: model.NextID(),
		TaskRunID:  run.TaskRunID,
		UserID:     1,
		AgentID:    "video_agent",
		SessionID:  "s1",
		StepName:   "video_agent",
		Status:     model.TaskStepStatusRunning,
		NeedWait:   model.NeedWaitYes,
	}
	testx.NoError(t, stepRepo.Insert(ctx, agentStep))

	// task_run → Waiting (simulating the state after the NeedWait=Yes branch)
	testx.NoError(t, taskRunRepo.UpdateByTaskRunID(ctx, run.TaskRunID, map[string]any{
		"status": model.TaskRunStatusWaiting,
	}))

	poller := taskflow.NewPoller(taskflow.PollerConfig{
		StepRepo:    stepRepo,
		TaskRunRepo: taskRunRepo,
		Logger:      zlog,
	})

	var frames []taskflow.TaskStepFrame
	done := poller.PollOnce(ctx, run.TaskRunID, "msg-1", func(f taskflow.TaskStepFrame) {
		frames = append(frames, f)
	})
	testx.True(t, done, "Waiting should also stop polling")
	for _, f := range frames {
		testx.NotEqual(t, sse.EventTaskDone, f.Type, "no task-done should be pushed during Waiting")
	}
}

// TestPoller_PollUntilDone verifies that PollUntilDone blocks until terminal and pushes all increments.
func TestPoller_PollUntilDone(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)

	run := &model.CGTaskRun{
		TaskRunID:     model.NextID(),
		UserID:        1,
		AgentID:       "gold_scriptwriter",
		SessionID:     "s1",
		OriginalQuery: "q",
		Status:        model.TaskRunStatusPending,
	}
	testx.NoError(t, taskRunRepo.Insert(ctx, run))

	// Simulate SubAgent execution: create steps in the background + terminate
	go func() {
		time.Sleep(300 * time.Millisecond)
		step := &model.CGTaskStep{
			TaskStepID: model.NextID(),
			TaskRunID:  run.TaskRunID,
			UserID:     1,
			AgentID:    "gold_scriptwriter",
			SessionID:  "s1",
			StepName:   "gold_scriptwriter",
			Status:     model.TaskStepStatusRunning,
		}
		_ = stepRepo.Insert(ctx, step)
		time.Sleep(300 * time.Millisecond)
		_ = stepRepo.UpdateByTaskStepID(ctx, step.TaskStepID, map[string]any{
			"status":      model.TaskStepStatusFinished,
			"step_output": `{"final_product":"海报已交付"}`,
		})
		_ = taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFinished, "")
	}()

	poller := taskflow.NewPoller(taskflow.PollerConfig{
		StepRepo:    stepRepo,
		TaskRunRepo: taskRunRepo,
		Logger:      zlog,
	})

	actx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var frames []taskflow.TaskStepFrame
	poller.PollUntilDone(actx, run.TaskRunID, "msg-1", func(f taskflow.TaskStepFrame) {
		frames = append(frames, f)
	}, 100*time.Millisecond)

	// Should push at least: step Running (task-step) + run terminal (task-done carrying the delivered product)
	testx.NotEmpty(t, frames)
	hasDone := false
	doneOutput := ""
	for _, f := range frames {
		if f.Type == sse.EventTaskDone {
			hasDone = true
			if f.Output != "" {
				doneOutput = f.Output
			}
		}
	}
	testx.True(t, hasDone, "a task-done should be received")
	testx.Contains(t, doneOutput, "海报已交付", "task-done should carry the delivered product")
}
