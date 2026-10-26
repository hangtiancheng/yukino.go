package taskflow_test

import (
	"context"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newCronPollerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGCronTask{}, &model.CGTaskRun{}))
	return db
}

// stubSubmitter 是测试用内存 Submitter（不走 Redis）。
type stubSubmitter struct {
	submitted []*model.CGTaskRun
}

func (s *stubSubmitter) Submit(ctx context.Context, run *model.CGTaskRun) (int64, error) {
	if run.TaskRunID == 0 {
		run.TaskRunID = model.NextID()
	}
	s.submitted = append(s.submitted, run)
	return run.TaskRunID, nil
}

// TestCronPoller_AdvanceStep 验证：当前 agent Finished 后推进到下一个 agent 并投递。
func TestCronPoller_AdvanceStep(t *testing.T) {
	ctx := context.Background()
	db := newCronPollerDB(t)
	cronRepo := repository.NewCronTaskRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := &stubSubmitter{}

	poller := taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
		CronRepo:    cronRepo,
		TaskRunRepo: taskRunRepo,
		Submitter:   submitter,
		Logger:      zap.NewNop(),
	})

	// 建 cron 任务：两个 agent
	cron := &model.CGCronTask{
		CronTaskID:       2001,
		SessionID:        "s-cron",
		MsgID:            "butler-2001", // 管家 message_id（首个 agent 由管家投递）
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"},{"agent_id":"gold_scriptwriter","input":"写脚本"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	// 当前步骤（idea_king）的 task_run 已 Finished（index==0 按管家 msg_id 查）
	testx.NoError(t, taskRunRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID:     3001,
		UserID:        1,
		AgentID:       "idea_king",
		SessionID:     "s-cron",
		MsgID:         "butler-2001",
		Status:        model.TaskRunStatusFinished,
		OriginalQuery: "选题",
	}))

	advanced := poller.AdvanceStep(ctx, cron)
	testx.True(t, advanced, "当前 agent 已完成应推进")
	testx.Len(t, submitter.submitted, 1, "应投递下一个 agent")
	plan := model.MustParsePlanSteps(submitter.submitted[0])
	testx.Len(t, plan.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", plan.Agents[0].AgentID, "第二个 agent")

	// 投递的 task_run msg_id 应遵循 "cron-{id}-{index}" 规则
	testx.Equal(t, "cron-2001-1", submitter.submitted[0].MsgID)

	// cron 记录 current_step_index 应翻到 1
	got, _ := cronRepo.GetByCronTaskID(ctx, 2001)
	testx.Equal(t, int32(1), got.CurrentStepIndex)
}

// TestCronPoller_AdvanceStep_IndexOne 验证：index>0 的当前 agent 按 "cron-{id}-{idx}" 规则查。
func TestCronPoller_AdvanceStep_IndexOne(t *testing.T) {
	ctx := context.Background()
	db := newCronPollerDB(t)
	cronRepo := repository.NewCronTaskRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := &stubSubmitter{}

	poller := taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
		CronRepo:    cronRepo,
		TaskRunRepo: taskRunRepo,
		Submitter:   submitter,
		Logger:      zap.NewNop(),
	})

	// 三个 agent，当前已推进到 index=1（gold_scriptwriter）
	cron := &model.CGCronTask{
		CronTaskID:       2011,
		SessionID:        "s-cron11",
		MsgID:            "butler-2011",
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"},{"agent_id":"gold_scriptwriter","input":"写脚本"},{"agent_id":"edit_master","input":"成片"}]}`,
		CurrentStepIndex: 1,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	// index=1 的 task_run 已 Finished（msg_id 符合 "cron-2011-1"）
	testx.NoError(t, taskRunRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID:     3011,
		UserID:        1,
		AgentID:       "gold_scriptwriter",
		SessionID:     "s-cron11",
		MsgID:         "cron-2011-1",
		Status:        model.TaskRunStatusFinished,
		OriginalQuery: "写脚本",
	}))

	advanced := poller.AdvanceStep(ctx, cron)
	testx.True(t, advanced, "index=1 已完成应推进")
	testx.Len(t, submitter.submitted, 1, "应投递第三个 agent")
	plan := model.MustParsePlanSteps(submitter.submitted[0])
	testx.Len(t, plan.Agents, 1)
	testx.Equal(t, "edit_master", plan.Agents[0].AgentID, "第三个 agent")
	testx.Equal(t, "cron-2011-2", submitter.submitted[0].MsgID)

	got, _ := cronRepo.GetByCronTaskID(ctx, 2011)
	testx.Equal(t, int32(2), got.CurrentStepIndex)
}

// TestCronPoller_NotFinished_Waits 验证：当前 agent 未完成时不推进、不投递。
func TestCronPoller_NotFinished_Waits(t *testing.T) {
	ctx := context.Background()
	db := newCronPollerDB(t)
	cronRepo := repository.NewCronTaskRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := &stubSubmitter{}

	poller := taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
		CronRepo:    cronRepo,
		TaskRunRepo: taskRunRepo,
		Submitter:   submitter,
		Logger:      zap.NewNop(),
	})

	cron := &model.CGCronTask{
		CronTaskID:       2002,
		SessionID:        "s-cron2",
		MsgID:            "butler-2002",
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"},{"agent_id":"gold_scriptwriter","input":"写脚本"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	// 当前 agent 的 task_run 是 Running（未完成）
	testx.NoError(t, taskRunRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID:     3002,
		UserID:        1,
		AgentID:       "idea_king",
		SessionID:     "s-cron2",
		MsgID:         "butler-2002",
		Status:        model.TaskRunStatusRunning,
		OriginalQuery: "选题",
	}))

	advanced := poller.AdvanceStep(ctx, cron)
	testx.False(t, advanced, "当前 agent 未完成不应推进")
	testx.Len(t, submitter.submitted, 0, "不应投递")

	// 索引不变
	got, _ := cronRepo.GetByCronTaskID(ctx, 2002)
	testx.Equal(t, int32(0), got.CurrentStepIndex)
}

// TestCronPoller_MissingRun_Waits 验证：当前 agent 的 task_run 不存在时等待（不误判）。
func TestCronPoller_MissingRun_Waits(t *testing.T) {
	ctx := context.Background()
	db := newCronPollerDB(t)
	cronRepo := repository.NewCronTaskRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := &stubSubmitter{}

	poller := taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
		CronRepo:    cronRepo,
		TaskRunRepo: taskRunRepo,
		Submitter:   submitter,
		Logger:      zap.NewNop(),
	})

	cron := &model.CGCronTask{
		CronTaskID:       2004,
		SessionID:        "s-cron4",
		MsgID:            "butler-2004",
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	// 不插入 task_run：当前 agent 未投递 → 等待
	advanced := poller.AdvanceStep(ctx, cron)
	testx.False(t, advanced, "当前 agent 的 task_run 不存在不应推进")
	testx.Len(t, submitter.submitted, 0, "不应投递")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2004)
	testx.Equal(t, int32(0), got.CurrentStepIndex)
	testx.Equal(t, model.CronTaskStatusRunning, got.Status, "未完成不应 MarkDone")
}

// TestCronPoller_AllDone 验证：最后一个 agent Finished 后 MarkDone 收口。
func TestCronPoller_AllDone(t *testing.T) {
	ctx := context.Background()
	db := newCronPollerDB(t)
	cronRepo := repository.NewCronTaskRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := &stubSubmitter{}

	poller := taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
		CronRepo:    cronRepo,
		TaskRunRepo: taskRunRepo,
		Submitter:   submitter,
		Logger:      zap.NewNop(),
	})

	// 单 agent cron：index=0 是最后一个
	cron := &model.CGCronTask{
		CronTaskID:       2003,
		SessionID:        "s-cron3",
		MsgID:            "butler-2003",
		PlanSteps:        `{"agents":[{"agent_id":"edit_master","input":"成片"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	testx.NoError(t, taskRunRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID:     3003,
		UserID:        1,
		AgentID:       "edit_master",
		SessionID:     "s-cron3",
		MsgID:         "butler-2003",
		Status:        model.TaskRunStatusFinished,
		OriginalQuery: "成片",
	}))

	advanced := poller.AdvanceStep(ctx, cron)
	testx.True(t, advanced, "最后一个 agent 完成应推进")
	testx.Len(t, submitter.submitted, 0, "无下一个 agent 不应投递")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2003)
	testx.Equal(t, model.CronTaskStatusDone, got.Status, "应 MarkDone")
}

// TestCronPoller_IndexOOB_MarkDone 验证：索引已越界（无对应 agent）时直接 MarkDone 收口。
func TestCronPoller_IndexOOB_MarkDone(t *testing.T) {
	ctx := context.Background()
	db := newCronPollerDB(t)
	cronRepo := repository.NewCronTaskRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := &stubSubmitter{}

	poller := taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
		CronRepo:    cronRepo,
		TaskRunRepo: taskRunRepo,
		Submitter:   submitter,
		Logger:      zap.NewNop(),
	})

	// 单 agent cron，index 已被推进到 1（越界）→ 直接收口
	cron := &model.CGCronTask{
		CronTaskID:       2005,
		SessionID:        "s-cron5",
		MsgID:            "butler-2005",
		PlanSteps:        `{"agents":[{"agent_id":"edit_master","input":"成片"}]}`,
		CurrentStepIndex: 1,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	advanced := poller.AdvanceStep(ctx, cron)
	testx.False(t, advanced, "越界收口不算推进")
	testx.Len(t, submitter.submitted, 0, "不应投递")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2005)
	testx.Equal(t, model.CronTaskStatusDone, got.Status, "越界应 MarkDone")
}

// TestCronPoller_BadPlan_MarksFailed 验证：plan_steps 解析失败时 MarkFailed。
func TestCronPoller_BadPlan_MarksFailed(t *testing.T) {
	ctx := context.Background()
	db := newCronPollerDB(t)
	cronRepo := repository.NewCronTaskRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := &stubSubmitter{}

	poller := taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
		CronRepo:    cronRepo,
		TaskRunRepo: taskRunRepo,
		Submitter:   submitter,
		Logger:      zap.NewNop(),
	})

	cron := &model.CGCronTask{
		CronTaskID:       2006,
		SessionID:        "s-cron6",
		MsgID:            "butler-2006",
		PlanSteps:        `{bad json`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	advanced := poller.AdvanceStep(ctx, cron)
	testx.False(t, advanced, "解析失败不推进")
	testx.Len(t, submitter.submitted, 0, "不应投递")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2006)
	testx.Equal(t, model.CronTaskStatusFailed, got.Status, "应 MarkFailed")
	testx.Equal(t, "plan_steps 解析失败", got.Reason)
}

// TestCronPoller_Scan 验证 Scan 遍历多个 Running 任务并推进可推进者。
func TestCronPoller_Scan(t *testing.T) {
	ctx := context.Background()
	db := newCronPollerDB(t)
	cronRepo := repository.NewCronTaskRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := &stubSubmitter{}

	poller := taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
		CronRepo:    cronRepo,
		TaskRunRepo: taskRunRepo,
		Submitter:   submitter,
		Logger:      zap.NewNop(),
	})

	// 任务 A：当前已完成 → 应推进
	cronA := &model.CGCronTask{
		CronTaskID:       2007,
		SessionID:        "s-cron7",
		MsgID:            "butler-2007",
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"},{"agent_id":"edit_master","input":"成片"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cronA))
	testx.NoError(t, taskRunRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID:     3007,
		UserID:        1,
		AgentID:       "idea_king",
		SessionID:     "s-cron7",
		MsgID:         "butler-2007",
		Status:        model.TaskRunStatusFinished,
		OriginalQuery: "选题",
	}))

	// 任务 B：当前未完成 → 不应推进
	cronB := &model.CGCronTask{
		CronTaskID:       2008,
		SessionID:        "s-cron8",
		MsgID:            "butler-2008",
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"},{"agent_id":"edit_master","input":"成片"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cronB))
	testx.NoError(t, taskRunRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID:     3008,
		UserID:        1,
		AgentID:       "idea_king",
		SessionID:     "s-cron8",
		MsgID:         "butler-2008",
		Status:        model.TaskRunStatusRunning,
		OriginalQuery: "选题",
	}))

	poller.Scan(ctx)
	testx.Len(t, submitter.submitted, 1, "只推进任务 A 的第二个 agent")
	testx.Equal(t, "edit_master", model.MustParsePlanSteps(submitter.submitted[0]).Agents[0].AgentID)

	gotA, _ := cronRepo.GetByCronTaskID(ctx, 2007)
	testx.Equal(t, int32(1), gotA.CurrentStepIndex)
	gotB, _ := cronRepo.GetByCronTaskID(ctx, 2008)
	testx.Equal(t, int32(0), gotB.CurrentStepIndex)
}

// TestCronPoller_FailedStep_MarksFailed 验证：当前 agent task_run 失败 → cron MarkFailed 收口（不再无限等待）。
func TestCronPoller_FailedStep_MarksFailed(t *testing.T) {
	ctx := context.Background()
	db := newCronPollerDB(t)
	cronRepo := repository.NewCronTaskRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := &stubSubmitter{}

	poller := taskflow.NewCronTaskPoller(taskflow.CronTaskPollerConfig{
		CronRepo:    cronRepo,
		TaskRunRepo: taskRunRepo,
		Submitter:   submitter,
		Logger:      zap.NewNop(),
	})

	cron := &model.CGCronTask{
		CronTaskID:       2009,
		SessionID:        "s-cron9",
		MsgID:            "butler-2009", // index==0 按 cron.MsgID 查
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"},{"agent_id":"gold_scriptwriter","input":"写脚本"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	testx.NoError(t, taskRunRepo.Insert(ctx, &model.CGTaskRun{
		TaskRunID:     3009,
		UserID:        1,
		AgentID:       "idea_king",
		SessionID:     "s-cron9",
		MsgID:         "butler-2009",
		Status:        model.TaskRunStatusFailed,
		OriginalQuery: "选题",
		Reason:        "模拟失败",
	}))

	advanced := poller.AdvanceStep(ctx, cron)
	testx.False(t, advanced, "失败不应推进")
	testx.Len(t, submitter.submitted, 0, "失败不应投递")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2009)
	testx.Equal(t, model.CronTaskStatusFailed, got.Status, "应 MarkFailed")
	testx.Contains(t, got.Reason, "模拟失败", "失败原因应透传")
}
