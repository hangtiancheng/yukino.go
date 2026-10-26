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

// newExecutorDB 创建内存 sqlite DB（迁移 task_run/task_step 两张表）。
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

// TestPoller_StreamsIncrementalSteps 验证 poll 只推增量 + 终态推 task-done。
func TestPoller_StreamsIncrementalSteps(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)

	// 建 task_run + 两个步骤
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

	// 第一轮：task_run 还 Pending → 只推 task-step，不推 task-done
	done := poller.PollOnce(ctx, run.TaskRunID, "msg-1", onFrame)
	testx.False(t, done, "task_run 未终态，不应停")
	testx.Len(t, frames, 1)
	testx.Equal(t, sse.EventTaskStep, frames[0].Type)

	// 推进：step1 → Finished + 写 step_output；task_run → Finished
	testx.NoError(t, stepRepo.UpdateByTaskStepID(ctx, step1.TaskStepID, map[string]any{
		"status":      model.TaskStepStatusFinished,
		"step_output": `{"final_product":"海报已交付"}`,
	}))
	testx.NoError(t, taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFinished, ""))

	// 第二轮：run 终态 → 推 task-done（携带最近 Finished 顶层步骤的输出）
	frames = nil
	done = poller.PollOnce(ctx, run.TaskRunID, "msg-1", onFrame)
	testx.True(t, done, "task_run Finished 后应停")
	testx.Len(t, frames, 1, "第二轮应推一条 run 终态 task-done")
	testx.Equal(t, sse.EventTaskDone, frames[0].Type)
	testx.Contains(t, frames[0].Output, "海报已交付", "task-done 应携带交付产物")
	testx.Equal(t, step1.TaskStepID, frames[0].TaskStepID)
}

// TestPoller_NeedWaitYes_StopsWithoutDone 验证 NeedWait=Yes：
// task_run 置 Waiting 时 poll 停止，但不推 task-done（终态未到，等产线回调）。
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

	// task_run → Waiting（模拟 NeedWait=Yes 分流后的状态）
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
	testx.True(t, done, "Waiting 也应停止轮询")
	for _, f := range frames {
		testx.NotEqual(t, sse.EventTaskDone, f.Type, "Waiting 阶段不应推 task-done")
	}
}

// TestPoller_PollUntilDone 验证 PollUntilDone 阻塞到终态并推完所有增量。
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

	// 模拟 SubAgent 执行：后台建步骤 + 终态化
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

	// 应至少推：step Running（task-step）+ run 终态（task-done 携带交付产物）
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
	testx.True(t, hasDone, "应收到 task-done")
	testx.Contains(t, doneOutput, "海报已交付", "task-done 应携带交付产物")
}
