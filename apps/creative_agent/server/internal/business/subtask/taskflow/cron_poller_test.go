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

// stubSubmitter is the in-memory Submitter for tests (no Redis).
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

// TestCronPoller_AdvanceStep verifies: after the current agent is Finished, advance to the next agent and dispatch.
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

	// Create the cron task: two agents
	cron := &model.CGCronTask{
		CronTaskID:       2001,
		SessionID:        "s-cron",
		MsgID:            "butler-2001", // manager message_id (the first agent is dispatched by the manager)
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"},{"agent_id":"gold_scriptwriter","input":"写脚本"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	// The current step's (idea_king) task_run is Finished (index==0 looked up by the manager msg_id)
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
	testx.True(t, advanced, "a finished current agent should advance")
	testx.Len(t, submitter.submitted, 1, "the next agent should be dispatched")
	plan := model.MustParsePlanSteps(submitter.submitted[0])
	testx.Len(t, plan.Agents, 1)
	testx.Equal(t, "gold_scriptwriter", plan.Agents[0].AgentID, "the second agent")

	// The dispatched task_run msg_id should follow the "cron-{id}-{index}" rule
	testx.Equal(t, "cron-2001-1", submitter.submitted[0].MsgID)

	// The cron record's current_step_index should flip to 1
	got, _ := cronRepo.GetByCronTaskID(ctx, 2001)
	testx.Equal(t, int32(1), got.CurrentStepIndex)
}

// TestCronPoller_AdvanceStep_IndexOne verifies: the current agent at index>0 is looked up by the "cron-{id}-{idx}" rule.
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

	// Three agents; currently advanced to index=1 (gold_scriptwriter)
	cron := &model.CGCronTask{
		CronTaskID:       2011,
		SessionID:        "s-cron11",
		MsgID:            "butler-2011",
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"},{"agent_id":"gold_scriptwriter","input":"写脚本"},{"agent_id":"edit_master","input":"成片"}]}`,
		CurrentStepIndex: 1,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, cronRepo.Insert(ctx, cron))

	// The index=1 task_run is Finished (msg_id matches "cron-2011-1")
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
	testx.True(t, advanced, "a finished index=1 should advance")
	testx.Len(t, submitter.submitted, 1, "the third agent should be dispatched")
	plan := model.MustParsePlanSteps(submitter.submitted[0])
	testx.Len(t, plan.Agents, 1)
	testx.Equal(t, "edit_master", plan.Agents[0].AgentID, "the third agent")
	testx.Equal(t, "cron-2011-2", submitter.submitted[0].MsgID)

	got, _ := cronRepo.GetByCronTaskID(ctx, 2011)
	testx.Equal(t, int32(2), got.CurrentStepIndex)
}

// TestCronPoller_NotFinished_Waits verifies: no advancement or dispatch while the current agent is unfinished.
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

	// The current agent's task_run is Running (unfinished)
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
	testx.False(t, advanced, "an unfinished current agent should not advance")
	testx.Len(t, submitter.submitted, 0, "nothing should be dispatched")

	// The index stays unchanged
	got, _ := cronRepo.GetByCronTaskID(ctx, 2002)
	testx.Equal(t, int32(0), got.CurrentStepIndex)
}

// TestCronPoller_MissingRun_Waits verifies: when the current agent's task_run does not exist, wait (no misjudgment).
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

	// Do not insert a task_run: the current agent was not dispatched → wait
	advanced := poller.AdvanceStep(ctx, cron)
	testx.False(t, advanced, "a missing current-agent task_run should not advance")
	testx.Len(t, submitter.submitted, 0, "nothing should be dispatched")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2004)
	testx.Equal(t, int32(0), got.CurrentStepIndex)
	testx.Equal(t, model.CronTaskStatusRunning, got.Status, "unfinished should not MarkDone")
}

// TestCronPoller_AllDone verifies: MarkDone closes out after the last agent is Finished.
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

	// Single-agent cron: index=0 is the last
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
	testx.True(t, advanced, "a finished last agent should advance")
	testx.Len(t, submitter.submitted, 0, "no next agent means no dispatch")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2003)
	testx.Equal(t, model.CronTaskStatusDone, got.Status, "it should MarkDone")
}

// TestCronPoller_IndexOOB_MarkDone verifies: an out-of-bounds index (no corresponding agent) closes out with MarkDone directly.
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

	// Single-agent cron with the index already advanced to 1 (out of bounds) → close out directly
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
	testx.False(t, advanced, "closing out on out-of-bounds does not count as advancement")
	testx.Len(t, submitter.submitted, 0, "nothing should be dispatched")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2005)
	testx.Equal(t, model.CronTaskStatusDone, got.Status, "out of bounds should MarkDone")
}

// TestCronPoller_BadPlan_MarksFailed verifies: MarkFailed when plan_steps fails to parse.
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
	testx.False(t, advanced, "a parse failure does not advance")
	testx.Len(t, submitter.submitted, 0, "nothing should be dispatched")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2006)
	testx.Equal(t, model.CronTaskStatusFailed, got.Status, "it should MarkFailed")
	testx.Equal(t, "failed to parse plan_steps", got.Reason)
}

// TestCronPoller_Scan verifies that Scan iterates multiple Running tasks and advances the advanceable ones.
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

	// Task A: current step finished → should advance
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

	// Task B: current step unfinished → should not advance
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
	testx.Len(t, submitter.submitted, 1, "only task A's second agent is advanced")
	testx.Equal(t, "edit_master", model.MustParsePlanSteps(submitter.submitted[0]).Agents[0].AgentID)

	gotA, _ := cronRepo.GetByCronTaskID(ctx, 2007)
	testx.Equal(t, int32(1), gotA.CurrentStepIndex)
	gotB, _ := cronRepo.GetByCronTaskID(ctx, 2008)
	testx.Equal(t, int32(0), gotB.CurrentStepIndex)
}

// TestCronPoller_FailedStep_MarksFailed verifies: the current agent's task_run failed → cron MarkFailed closes out (no infinite waiting).
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
		MsgID:            "butler-2009", // index==0 is looked up by cron.MsgID
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
	testx.False(t, advanced, "a failure should not advance")
	testx.Len(t, submitter.submitted, 0, "a failure should not dispatch")

	got, _ := cronRepo.GetByCronTaskID(ctx, 2009)
	testx.Equal(t, model.CronTaskStatusFailed, got.Status, "it should MarkFailed")
	testx.Contains(t, got.Reason, "模拟失败", "the failure reason should be passed through")
}
