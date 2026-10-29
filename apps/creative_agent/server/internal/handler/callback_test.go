package handler_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/handler"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/baseapi"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newCallbackTestDB creates an in-memory sqlite DB with the task_run + task_step tables registered.
func newCallbackTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGTaskRun{}, &model.CGTaskStep{}))
	return db
}

// TestAdvanceTaskOnCallback_WaitingToFinished verifies that a callback advances a Waiting task to Finished.
func TestAdvanceTaskOnCallback_WaitingToFinished(t *testing.T) {
	db := newCallbackTestDB(t)
	stepRepo := repository.NewTaskStepRepository(db)
	runRepo := repository.NewTaskRunRepository(db)

	// Create a Waiting task_run + one agent top-level step (parent_step_id=0)
	taskRunID := int64(1001)
	agentStepID := int64(2001)
	testx.NoError(t, runRepo.Insert(context.Background(), &model.CGTaskRun{
		TaskRunID: taskRunID,
		UserID:    1,
		AgentID:   "edit_master",
		SessionID: "s1",
		Status:    model.TaskRunStatusWaiting,
	}))
	testx.NoError(t, stepRepo.Insert(context.Background(), &model.CGTaskStep{
		TaskStepID:   agentStepID,
		TaskRunID:    taskRunID,
		ParentStepID: 0,
		UserID:       1,
		AgentID:      "edit_master",
		SessionID:    "s1",
		Status:       model.TaskStepStatusRunning,
		NeedWait:     model.NeedWaitYes,
	}))

	// Build the callback (project_id is the session_id).
	// Inject a FindWaitingRun mock: covers the "externally injected locator" branch (see TestAdvanceTaskOnCallback_DefaultPath_Idempotent for the default path).
	err := handler.AdvanceTaskOnCallback(context.Background(), handler.CallbackDeps{
		StepRepo:    stepRepo,
		TaskRunRepo: runRepo,
		FindWaitingRun: func(ctx context.Context, cb *baseapi.CallbackReq) (*model.CGTaskRun, error) {
			return runRepo.GetByTaskRunID(ctx, taskRunID)
		},
	}, &baseapi.CallbackReq{
		TaskID:      42,
		ProjectID:   "s1",
		UserID:      1,
		TaskStatus:  "success",
		SpecialInfo: json.RawMessage(`{"images":["cf://fake/img.png"]}`),
	})
	testx.NoError(t, err)

	// Assert the task_run is Finished
	run, err := runRepo.GetByTaskRunID(context.Background(), taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFinished, run.Status)

	// Assert the agent step is Finished + step_output is non-empty
	steps, err := stepRepo.GetByTaskRunID(context.Background(), taskRunID)
	testx.NoError(t, err)
	var agentStep *model.CGTaskStep
	for i := range steps {
		if steps[i].TaskStepID == agentStepID {
			agentStep = &steps[i]
		}
	}
	testx.NotNil(t, agentStep)
	testx.Equal(t, model.TaskStepStatusFinished, agentStep.Status)
	testx.NotEmpty(t, agentStep.StepOutput, "step_output should contain the terminal product")
}

// TestAdvanceTaskOnCallback_DefaultPath verifies the default path (no injected FindWaitingRun) goes through
// findWaitingRunDefault → TaskRunRepo.GetWaitingBySession: located by project_id(session_id) + user_id.
func TestAdvanceTaskOnCallback_DefaultPath(t *testing.T) {
	db := newCallbackTestDB(t)
	stepRepo := repository.NewTaskStepRepository(db)
	runRepo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	taskRunID := int64(3001)
	agentStepID := int64(3002)
	testx.NoError(t, runRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID: taskRunID,
		UserID:    1,
		AgentID:   "edit_master",
		SessionID: "s1",
		Status:    model.TaskRunStatusWaiting,
	}))
	testx.NoError(t, stepRepo.Insert(ctx, &model.CGTaskStep{
		TaskStepID:   agentStepID,
		TaskRunID:    taskRunID,
		ParentStepID: 0,
		UserID:       1,
		AgentID:      "edit_master",
		SessionID:    "s1",
		Status:       model.TaskStepStatusRunning,
		NeedWait:     model.NeedWaitYes,
	}))

	// No injected FindWaitingRun: uses the default GetWaitingBySession (project_id = session_id)
	err := handler.AdvanceTaskOnCallback(ctx, handler.CallbackDeps{
		StepRepo:    stepRepo,
		TaskRunRepo: runRepo,
	}, &baseapi.CallbackReq{
		TaskID:      42,
		ProjectID:   "s1",
		UserID:      1,
		TaskStatus:  "success",
		SpecialInfo: json.RawMessage(`{"images":["cf://fake/img.png"]}`),
	})
	testx.NoError(t, err)

	run, err := runRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFinished, run.Status)

	step, err := stepRepo.GetByTaskStepID(ctx, agentStepID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusFinished, step.Status)
}

// TestAdvanceTaskOnCallback_Idempotent verifies that duplicate callbacks have no side effects:
// on the second callback the run is already Finished, GetWaitingBySession finds no Waiting → idempotent skip (returns nil).
func TestAdvanceTaskOnCallback_Idempotent(t *testing.T) {
	db := newCallbackTestDB(t)
	stepRepo := repository.NewTaskStepRepository(db)
	runRepo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	taskRunID := int64(4001)
	agentStepID := int64(4002)
	cb := &baseapi.CallbackReq{
		TaskID:      42,
		ProjectID:   "s1",
		UserID:      1,
		TaskStatus:  "success",
		SpecialInfo: json.RawMessage(`{"images":["cf://fake/img.png"]}`),
	}
	deps := handler.CallbackDeps{
		StepRepo:    stepRepo,
		TaskRunRepo: runRepo,
	}

	testx.NoError(t, runRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID: taskRunID,
		UserID:    1,
		AgentID:   "edit_master",
		SessionID: "s1",
		Status:    model.TaskRunStatusWaiting,
	}))
	testx.NoError(t, stepRepo.Insert(ctx, &model.CGTaskStep{
		TaskStepID:   agentStepID,
		TaskRunID:    taskRunID,
		ParentStepID: 0,
		UserID:       1,
		AgentID:      "edit_master",
		SessionID:    "s1",
		Status:       model.TaskStepStatusRunning,
		NeedWait:     model.NeedWaitYes,
	}))

	// First callback: Waiting→Finished, step→Finished
	testx.NoError(t, handler.AdvanceTaskOnCallback(ctx, deps, cb))

	// Record step_output after the first callback; the second callback must not change it
	step, err := stepRepo.GetByTaskStepID(ctx, agentStepID)
	testx.NoError(t, err)
	firstOutput := step.StepOutput

	// Second (duplicate) callback: no side effects (returns nil, run stays Finished, step unchanged)
	err = handler.AdvanceTaskOnCallback(ctx, deps, cb)
	testx.NoError(t, err, "a duplicate callback should idempotently return nil instead of an error")

	run, err := runRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFinished, run.Status, "the run should stay Finished after a duplicate callback")

	step, err = stepRepo.GetByTaskStepID(ctx, agentStepID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusFinished, step.Status)
	testx.Equal(t, firstOutput, step.StepOutput, "a duplicate callback should not rewrite step_output")
	testx.NotEmpty(t, step.StepOutput, "the first callback should have written step_output")
}

// TestAdvanceTaskOnCallback_AnchorOnlyNeedWaitRunning verifies the anchoring logic:
// only top-level steps with NeedWait=Yes and Running are updated; top-level steps that are not the Waiting anchor are never touched
// (avoiding wrong-step updates across sessions / duplicate callbacks).
func TestAdvanceTaskOnCallback_AnchorOnlyNeedWaitRunning(t *testing.T) {
	db := newCallbackTestDB(t)
	stepRepo := repository.NewTaskStepRepository(db)
	runRepo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	taskRunID := int64(5001)
	// Two top-level steps: the first with NeedWait=No (already finished, must not be anchored),
	// the second with NeedWait=Yes and Running (the real anchor waiting for the callback).
	doneStepID := int64(5002)
	waitStepID := int64(5003)
	testx.NoError(t, runRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID: taskRunID,
		UserID:    1,
		AgentID:   "edit_master",
		SessionID: "s1",
		Status:    model.TaskRunStatusWaiting,
	}))
	for _, s := range []*model.CGTaskStep{
		{TaskStepID: doneStepID, TaskRunID: taskRunID, ParentStepID: 0, UserID: 1,
			AgentID: "script_writer", SessionID: "s1", Status: model.TaskStepStatusFinished, NeedWait: model.NeedWaitNo,
			StepOutput: `{"final_product":"old product"}`},
		{TaskStepID: waitStepID, TaskRunID: taskRunID, ParentStepID: 0, UserID: 1,
			AgentID: "edit_master", SessionID: "s1", Status: model.TaskStepStatusRunning, NeedWait: model.NeedWaitYes},
	} {
		testx.NoError(t, stepRepo.Insert(ctx, s))
	}

	// Inject a FindWaitingRun mock returning the run directly (focus on the anchoring logic, bypass default locating)
	err := handler.AdvanceTaskOnCallback(ctx, handler.CallbackDeps{
		StepRepo:    stepRepo,
		TaskRunRepo: runRepo,
		FindWaitingRun: func(ctx context.Context, cb *baseapi.CallbackReq) (*model.CGTaskRun, error) {
			return runRepo.GetByTaskRunID(ctx, taskRunID)
		},
	}, &baseapi.CallbackReq{
		TaskID:      42,
		ProjectID:   "s1",
		UserID:      1,
		TaskStatus:  "success",
		SpecialInfo: json.RawMessage(`{"images":["cf://fake/img.png"]}`),
	})
	testx.NoError(t, err)

	// The anchored step (NeedWait=Yes and Running) → Finished + step_output
	waitStep, err := stepRepo.GetByTaskStepID(ctx, waitStepID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusFinished, waitStep.Status)
	testx.Contains(t, waitStep.StepOutput, "final_product", "the anchored step should contain the callback product")

	// The non-anchored step (NeedWait=No and already Finished) → unchanged
	doneStep, err := stepRepo.GetByTaskStepID(ctx, doneStepID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusFinished, doneStep.Status)
	testx.Contains(t, doneStep.StepOutput, "old product", "the non-anchored top-level step should not be modified")
}

// TestAdvanceTaskOnCallback_NoAnchorStep verifies that without an anchored step (no top-level step with NeedWait=Yes and Running)
// the run is not advanced to Finished: it stays Waiting, avoiding the "Finished but no product" zombie state.
func TestAdvanceTaskOnCallback_NoAnchorStep(t *testing.T) {
	db := newCallbackTestDB(t)
	stepRepo := repository.NewTaskStepRepository(db)
	runRepo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	taskRunID := int64(6001)
	// Only one top-level step with NeedWait=No: it must not be anchored
	testx.NoError(t, runRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID: taskRunID,
		UserID:    1,
		AgentID:   "edit_master",
		SessionID: "s1",
		Status:    model.TaskRunStatusWaiting,
	}))
	testx.NoError(t, stepRepo.Insert(ctx, &model.CGTaskStep{
		TaskStepID:   6002,
		TaskRunID:    taskRunID,
		ParentStepID: 0,
		UserID:       1,
		AgentID:      "edit_master",
		SessionID:    "s1",
		Status:       model.TaskStepStatusRunning,
		NeedWait:     model.NeedWaitNo,
	}))

	err := handler.AdvanceTaskOnCallback(ctx, handler.CallbackDeps{
		StepRepo:    stepRepo,
		TaskRunRepo: runRepo,
		FindWaitingRun: func(ctx context.Context, cb *baseapi.CallbackReq) (*model.CGTaskRun, error) {
			return runRepo.GetByTaskRunID(ctx, taskRunID)
		},
	}, &baseapi.CallbackReq{
		TaskID:      42,
		ProjectID:   "s1",
		UserID:      1,
		TaskStatus:  "success",
		SpecialInfo: json.RawMessage(`{"images":["cf://fake/img.png"]}`),
	})
	testx.NoError(t, err, "without an anchored step it should idempotently return nil (no error)")

	// The run should stay Waiting, not advance to Finished
	run, err := runRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusWaiting, run.Status, "the run should not advance to Finished without an anchored step")

	// The step should also stay Running with no step_output
	step, err := stepRepo.GetByTaskStepID(ctx, 6002)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusRunning, step.Status)
	testx.Empty(t, step.StepOutput)
}
