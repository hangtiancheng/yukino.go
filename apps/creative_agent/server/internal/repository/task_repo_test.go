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

// newTaskTestDB 创建内存 sqlite 数据库，注册 task_run + task_step 两张表。
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

// TestTaskRun_Insert_Get 验证 Insert → GetByTaskRunID 往返。
func TestTaskRun_Insert_Get(t *testing.T) {
	db := newTaskTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	run := newTaskRun("gold_scriptwriter", "s1")
	testx.NoError(t, repo.Insert(ctx, run))
	testx.NotZero(t, run.ID, "自增主键应回填")

	got, err := repo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Equal(t, run.TaskRunID, got.TaskRunID)
	testx.Equal(t, model.TaskRunStatusPending, got.Status)
	testx.Contains(t, got.PlanSteps, "gold_scriptwriter")

	// 不存在 → ErrTaskRunNotFound
	_, err = repo.GetByTaskRunID(ctx, model.NextID())
	testx.ErrorIs(t, err, repository.ErrTaskRunNotFound)
}

// TestTaskRun_UpdateCAS 验证 CAS 抢占语义：只有 status 匹配时才更新成功。
func TestTaskRun_UpdateCAS(t *testing.T) {
	db := newTaskTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	run := newTaskRun("gold_scriptwriter", "s1")
	testx.NoError(t, repo.Insert(ctx, run))

	// 抢占 Pending→Running：应该成功
	ok, err := repo.UpdateCAS(ctx, run.TaskRunID, model.TaskRunStatusPending, map[string]any{
		"status":     model.TaskRunStatusRunning,
		"start_time": timeNowMs(),
	})
	testx.NoError(t, err)
	testx.True(t, ok, "Pending→Running 抢占应成功")

	// 再次抢占 Pending→Running：CAS 冲突（当前已是 Running）
	ok2, err := repo.UpdateCAS(ctx, run.TaskRunID, model.TaskRunStatusPending, map[string]any{
		"status": model.TaskRunStatusRunning,
	})
	testx.NoError(t, err)
	testx.False(t, ok2, "再次 Pending→Running 应失败（状态已变）")

	// 校验最终状态
	got, err := repo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusRunning, got.Status)
	testx.NotZero(t, got.StartTime)
}

// TestTaskRun_MarkTerminal 验证终态标记会写 end_time。
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

// TestTaskStep_Insert_Tree 验证 task_step 的树形结构插入与按 task_run 查询。
func TestTaskStep_Insert_Tree(t *testing.T) {
	db := newTaskTestDB(t)
	runRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)
	ctx := context.Background()

	run := newTaskRun("gold_scriptwriter", "s1")
	testx.NoError(t, runRepo.Insert(ctx, run))

	// 顶层步骤（子智能体级）
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

	// 工具调用子步骤（parent=agentStep）
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

	// 按 task_run 查询：应同时返回顶层与子步骤
	steps, err := stepRepo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Len(t, steps, 2)

	// 更新子步骤输出
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

// TestTaskStep_GetNewestFinishedOutput 验证「最近一条 Finished 顶层步骤输出」查询
// （NeedWait 分支最终交付产物）。
func TestTaskStep_GetNewestFinishedOutput(t *testing.T) {
	db := newTaskTestDB(t)
	runRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)
	ctx := context.Background()

	run := newTaskRun("video_agent", "s3")
	testx.NoError(t, runRepo.Insert(ctx, run))

	// 两条顶层步骤：第一条 Running（中间结果），第二条 Finished（交付）
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
	testx.Contains(t, out, "v/1.mp4", "应返回最新一条 Finished 的输出")
}
