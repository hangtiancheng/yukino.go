package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func timeNowMs() int64 { return time.Now().UnixMilli() }

// newTaskTestDB creates an in-memory sqlite database with the task_run + task_step tables registered.
func newTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGTaskRun{}, &model.CGTaskStep{}))
	return db
}

func newTaskRun(agentID, sessionID string) *model.CGTaskRun {
	return &model.CGTaskRun{
		TaskRunID:     model.NextID(),
		UserID:        1,
		AgentID:       agentID,
		SessionID:     sessionID,
		OriginalQuery: "生成一张海报",
		PlanSteps:     `{"agents":[{"agent_id":"gold_scriptwriter","input":"写脚本"}]}`,
		Status:        model.TaskRunStatusPending,
	}
}

// TestTaskRun_Insert_Get verifies the Insert → GetByTaskRunID round trip.
func TestTaskRun_Insert_Get(t *testing.T) {
	db := newTaskTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	run := newTaskRun("gold_scriptwriter", "s1")
	testx.NoError(t, repo.Insert(ctx, run))
	testx.NotZero(t, run.ID, "the auto-increment primary key should be backfilled")

	got, err := repo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Equal(t, run.TaskRunID, got.TaskRunID)
	testx.Equal(t, model.TaskRunStatusPending, got.Status)
	testx.Contains(t, got.PlanSteps, "gold_scriptwriter")

	// Not found → ErrTaskRunNotFound
	_, err = repo.GetByTaskRunID(ctx, model.NextID())
	testx.ErrorIs(t, err, repository.ErrTaskRunNotFound)
}

// TestTaskRun_UpdateCAS verifies CAS claim semantics: the update succeeds only when the status matches.
func TestTaskRun_UpdateCAS(t *testing.T) {
	db := newTaskTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	run := newTaskRun("gold_scriptwriter", "s1")
	testx.NoError(t, repo.Insert(ctx, run))

	// Claim Pending→Running: should succeed
	ok, err := repo.UpdateCAS(ctx, run.TaskRunID, model.TaskRunStatusPending, map[string]any{
		"status":     model.TaskRunStatusRunning,
		"start_time": timeNowMs(),
	})
	testx.NoError(t, err)
	testx.True(t, ok, "the Pending→Running claim should succeed")

	// Claim Pending→Running again: CAS conflict (already Running)
	ok2, err := repo.UpdateCAS(ctx, run.TaskRunID, model.TaskRunStatusPending, map[string]any{
		"status": model.TaskRunStatusRunning,
	})
	testx.NoError(t, err)
	testx.False(t, ok2, "a second Pending→Running claim should fail (status changed)")

	// Verify the final status
	got, err := repo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusRunning, got.Status)
	testx.NotZero(t, got.StartTime)
}

// TestTaskRun_MarkTerminal verifies that marking terminal writes end_time.
func TestTaskRun_MarkTerminal(t *testing.T) {
	db := newTaskTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	run := newTaskRun("edit_master", "s2")
	testx.NoError(t, repo.Insert(ctx, run))

	testx.NoError(t, repo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFinished, ""))
	got, err := repo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFinished, got.Status)
	testx.NotZero(t, got.EndTime)
}

// TestTaskStep_Insert_Tree verifies tree-structured task_step inserts and querying by task_run.
func TestTaskStep_Insert_Tree(t *testing.T) {
	db := newTaskTestDB(t)
	runRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)
	ctx := context.Background()

	run := newTaskRun("gold_scriptwriter", "s1")
	testx.NoError(t, runRepo.Insert(ctx, run))

	// Top-level step (sub-agent level)
	agentStep := &model.CGTaskStep{
		TaskStepID:   model.NextID(),
		TaskRunID:    run.TaskRunID,
		ParentStepID: 0,
		UserID:       1,
		AgentID:      run.AgentID,
		SessionID:    run.SessionID,
		StepName:     "gold_scriptwriter",
		StepInput:    `{"query":"写脚本"}`,
		Status:       model.TaskStepStatusRunning,
	}
	testx.NoError(t, stepRepo.Insert(ctx, agentStep))

	// Tool-call sub-step (parent=agentStep)
	toolStep := &model.CGTaskStep{
		TaskStepID:   model.NextID(),
		TaskRunID:    run.TaskRunID,
		ParentStepID: agentStep.TaskStepID,
		UserID:       1,
		AgentID:      run.AgentID,
		SessionID:    run.SessionID,
		StepName:     "generate_image",
		StepInput:    `{"prompt":"海报"}`,
		Status:       model.TaskStepStatusRunning,
	}
	testx.NoError(t, stepRepo.Insert(ctx, toolStep))

	// Query by task_run: both the top-level and sub-steps should be returned
	steps, err := stepRepo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Len(t, steps, 2)

	// Update the sub-step output
	testx.NoError(t, stepRepo.UpdateByTaskStepID(ctx, toolStep.TaskStepID, map[string]any{
		"status":              model.TaskStepStatusFinished,
		"step_output":         `{"image":"http://img/1.png"}`,
		"intermediate_result": `{"image":"http://img/1.png"}`,
	}))

	got, err := stepRepo.GetByTaskStepID(ctx, toolStep.TaskStepID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusFinished, got.Status)
	testx.Contains(t, got.StepOutput, "img/1.png")
}

// TestTaskStep_GetNewestFinishedOutput verifies the "newest Finished top-level step output" query
// (the final delivered product of the NeedWait branch).
func TestTaskStep_GetNewestFinishedOutput(t *testing.T) {
	db := newTaskTestDB(t)
	runRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)
	ctx := context.Background()

	run := newTaskRun("video_agent", "s3")
	testx.NoError(t, runRepo.Insert(ctx, run))

	// Two top-level steps: the first Running (intermediate result), the second Finished (delivered)
	for i, status := range []int32{model.TaskStepStatusRunning, model.TaskStepStatusFinished} {
		testx.NoError(t, stepRepo.Insert(ctx, &model.CGTaskStep{
			TaskStepID:   model.NextID(),
			TaskRunID:    run.TaskRunID,
			ParentStepID: 0,
			UserID:       1,
			AgentID:      run.AgentID,
			SessionID:    run.SessionID,
			StepName:     "step" + string(rune('0'+i)),
			Status:       status,
			StepOutput:   `{"video":"http://v/` + string(rune('0'+i)) + `.mp4"}`,
		}))
	}

	out, err := stepRepo.GetNewestFinishedOutput(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Contains(t, out, "v/1.mp4", "the output of the newest Finished step should be returned")
}
