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

// newCallbackTestDB 创建内存 sqlite，注册 task_run + task_step 两张表。
func newCallbackTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGTaskRun{}, &model.CGTaskStep{}))
	return db
}

// TestAdvanceTaskOnCallback_WaitingToFinished 验证回调把 Waiting 任务推进为 Finished。
func TestAdvanceTaskOnCallback_WaitingToFinished(t *testing.T) {
	db := newCallbackTestDB(t)
	stepRepo := repository.NewTaskStepRepository(db)
	runRepo := repository.NewTaskRunRepository(db)

	// 建一个 Waiting 的 task_run + 一个 agent 顶层 step（parent_step_id=0）
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

	// 构造回调（project_id 即 session_id）。
	// FindWaitingRun 注入 mock：覆盖「外部注入定位器」分支（默认路径见 TestAdvanceTaskOnCallback_DefaultPath_Idempotent）。
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

	// 断言 task_run Finished
	run, err := runRepo.GetByTaskRunID(context.Background(), taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFinished, run.Status)

	// 断言 agent step Finished + step_output 非空
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
	testx.NotEmpty(t, agentStep.StepOutput, "step_output 应写入终态产物")
}

// TestAdvanceTaskOnCallback_DefaultPath 验证默认路径（不注入 FindWaitingRun）走
// findWaitingRunDefault → TaskRunRepo.GetWaitingBySession：按 project_id(session_id) + user_id 定位。
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

	// 不注入 FindWaitingRun：走默认 GetWaitingBySession（project_id = session_id）
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

// TestAdvanceTaskOnCallback_Idempotent 验证重复回调无副作用：
// 第二次回调时 run 已 Finished，GetWaitingBySession 查不到 Waiting → 幂等跳过（返回 nil）。
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

	// 第一次回调：Waiting→Finished，step→Finished
	testx.NoError(t, handler.AdvanceTaskOnCallback(ctx, deps, cb))

	// 记录第一次后的 step_output，第二次回调不应改动它
	step, err := stepRepo.GetByTaskStepID(ctx, agentStepID)
	testx.NoError(t, err)
	firstOutput := step.StepOutput

	// 第二次回调（重复）：应无副作用（返回 nil，run 保持 Finished，step 不改动）
	err = handler.AdvanceTaskOnCallback(ctx, deps, cb)
	testx.NoError(t, err, "重复回调应幂等返回 nil，而不是报错")

	run, err := runRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFinished, run.Status, "重复回调后 run 应保持 Finished")

	step, err = stepRepo.GetByTaskStepID(ctx, agentStepID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusFinished, step.Status)
	testx.Equal(t, firstOutput, step.StepOutput, "重复回调不应改写 step_output")
	testx.NotEmpty(t, step.StepOutput, "第一次回调应已写入 step_output")
}

// TestAdvanceTaskOnCallback_AnchorOnlyNeedWaitRunning 验证锚定逻辑：
// 只更新 NeedWait=Yes 且 Running 的顶层 step，绝不动非 Waiting 锚点的顶层 step
// （跨会话/重复回调场景下避免改错 step）。
func TestAdvanceTaskOnCallback_AnchorOnlyNeedWaitRunning(t *testing.T) {
	db := newCallbackTestDB(t)
	stepRepo := repository.NewTaskStepRepository(db)
	runRepo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	taskRunID := int64(5001)
	// 两个顶层 step：第一个 NeedWait=No（已完成，不应被锚定），
	// 第二个 NeedWait=Yes 且 Running（真正等回调的锚点）。
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
			StepOutput: `{"final_product":"旧产物"}`},
		{TaskStepID: waitStepID, TaskRunID: taskRunID, ParentStepID: 0, UserID: 1,
			AgentID: "edit_master", SessionID: "s1", Status: model.TaskStepStatusRunning, NeedWait: model.NeedWaitYes},
	} {
		testx.NoError(t, stepRepo.Insert(ctx, s))
	}

	// 注入 FindWaitingRun mock，直接返回该 run（聚焦锚定逻辑，不绕默认定位）
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

	// 锚定的 step（NeedWait=Yes 且 Running）→ Finished + step_output
	waitStep, err := stepRepo.GetByTaskStepID(ctx, waitStepID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusFinished, waitStep.Status)
	testx.Contains(t, waitStep.StepOutput, "final_product", "锚定的 step 应写入回调产物")

	// 非锚定 step（NeedWait=No 且已 Finished）→ 原样不动
	doneStep, err := stepRepo.GetByTaskStepID(ctx, doneStepID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusFinished, doneStep.Status)
	testx.Contains(t, doneStep.StepOutput, "旧产物", "非锚定顶层 step 不应被改动")
}

// TestAdvanceTaskOnCallback_NoAnchorStep 验证无锚定 step（没有 NeedWait=Yes 且 Running 的顶层 step）时
// 不推进 Finished：run 保持 Waiting，避免"Finished 但无产物"的僵尸态。
func TestAdvanceTaskOnCallback_NoAnchorStep(t *testing.T) {
	db := newCallbackTestDB(t)
	stepRepo := repository.NewTaskStepRepository(db)
	runRepo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	taskRunID := int64(6001)
	// 只有一条 NeedWait=No 的顶层 step：不应被锚定
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
	testx.NoError(t, err, "无锚定 step 时应幂等返回 nil（不报错）")

	// run 应保持 Waiting，不推进 Finished
	run, err := runRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusWaiting, run.Status, "无锚定 step 时 run 不应推进为 Finished")

	// step 也应保持 Running、无 step_output
	step, err := stepRepo.GetByTaskStepID(ctx, 6002)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskStepStatusRunning, step.Status)
	testx.Empty(t, step.StepOutput)
}
