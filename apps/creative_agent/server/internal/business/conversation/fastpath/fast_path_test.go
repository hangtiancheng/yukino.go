package fastpath_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/fastpath"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/executor"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newFastPathDB 创建内存 sqlite，注册 cg_task_run 表。
func newFastPathDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGTaskRun{}))
	return db
}

// stubStream 是测试用内存 StreamQ stub（实现 infra.StreamQueue 接口）。
type stubStream struct{}

func (stubStream) Push(ctx context.Context, msgID string, body []byte) error { return nil }
func (stubStream) Receive(ctx context.Context) (*infra.StreamMessage, error) { return nil, nil }
func (stubStream) Ack(ctx context.Context, streamMsgID string) error         { return nil }
func (stubStream) Requeue(ctx context.Context, msgID string, body []byte) error {
	return nil
}
func (stubStream) ClaimPending(ctx context.Context, minIdle time.Duration) ([]*infra.StreamMessage, error) {
	return nil, nil
}

func TestFastPath_CanFastTask(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()
	taskRunRepo := repository.NewTaskRunRepository(db)

	// 内存 stream stub
	stream := &stubStream{}
	submitter := subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
		TaskRunRepo: taskRunRepo,
		StreamQ:     stream,
		Logger:      zap.NewNop(),
	})

	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   submitter,
		TaskRunRepo: taskRunRepo,
		Logger:      zap.NewNop(),
	})

	// script_confirmed 命中 → handled + 投递
	mem.SetPipeline("s1", "mix_video")
	mem.SetStage("s1", memory.FlowStageScriptConfirmed)
	mem.SetAgent("s1", "gold_scriptwriter")

	handled, err := fp.CheckFastTask(ctx, &fastpath.FastTaskRequest{
		SessionID: "s1",
		UserID:    1,
		AgentID:   "manager",
		Query:     "继续",
	})
	testx.NoError(t, err)
	testx.True(t, handled, "script_confirmed 应命中快任务")

	// 投递了新的 execute_agent task_run（链中下一个 = edit_master）
	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 1, "应投递一个 task_run")
	plan, err := runs[0].ParsePlanSteps()
	testx.NoError(t, err)
	testx.Len(t, plan.Agents, 1)
	testx.Equal(t, "edit_master", plan.Agents[0].AgentID)
}

func TestFastPath_NoMatch_EmptyStage(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      memory.NewIntentMemory(),
		Submitter:   subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{TaskRunRepo: repository.NewTaskRunRepository(db), StreamQ: &stubStream{}, Logger: zap.NewNop()}),
		TaskRunRepo: repository.NewTaskRunRepository(db),
		Logger:      zap.NewNop(),
	})

	handled, err := fp.CheckFastTask(ctx, &fastpath.FastTaskRequest{
		SessionID: "s1", UserID: 1, AgentID: "manager", Query: "生成一张海报",
	})
	testx.NoError(t, err)
	testx.False(t, handled, "FlowStageEmpty 不应命中")
}

func TestFastPath_Retry(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()

	// 预置一条失败 task_run（模拟上次失败）
	failedRun := &model.CGTaskRun{
		TaskRunID:     model.NextID(),
		UserID:        1,
		AgentID:       "manager",
		SessionID:     "s1",
		OriginalQuery: "生成海报",
		Status:        model.TaskRunStatusFailed,
	}
	testx.NoError(t, failedRun.SetPlanSteps(&model.PlanSteps{Agents: []model.PlanAgent{
		{AgentID: "idea_king", Input: "生成海报"},
	}}))
	testx.NoError(t, db.Create(failedRun).Error)
	mem.RecordFailedTaskRun("s1", failedRun.TaskRunID)

	taskRunRepo := repository.NewTaskRunRepository(db)
	submitter := subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
		TaskRunRepo: taskRunRepo,
		StreamQ:     &stubStream{},
		Logger:      zap.NewNop(),
	})
	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   submitter,
		TaskRunRepo: taskRunRepo,
		Logger:      zap.NewNop(),
	})

	handled, err := fp.CheckFastTask(ctx, &fastpath.FastTaskRequest{
		SessionID:   "s1",
		UserID:      1,
		AgentID:     "manager",
		Query:       "重试",
		RequestType: "retry",
	})
	testx.NoError(t, err)
	testx.True(t, handled, "retry 请求应命中")

	// 投递了新的 task_run（继承失败 run 的 plan）
	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 2, "应有一条新 task_run")
}

func TestFastPath_HILResume(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()

	// 预置 checkpoint
	cp := &react.Checkpoint{
		Messages: []react.Message{{Role: "user", Content: "继续"}},
		Step:     1,
		Query:    "原 query",
	}
	mem.SaveCheckpoint("s1", cp)

	taskRunRepo := repository.NewTaskRunRepository(db)
	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   nil, // resume 不投递，Submitter 可空
		TaskRunRepo: taskRunRepo,
		Logger:      zap.NewNop(),
	})

	handled, err := fp.CheckFastTask(ctx, &fastpath.FastTaskRequest{
		SessionID: "s1",
		UserID:    1,
		AgentID:   "manager",
		Query:     "继续",
		Resume:    true,
	})
	testx.NoError(t, err)
	testx.True(t, handled, "resume 且存在 checkpoint 应命中")
}

func TestFastPath_ResumePrecedence(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()

	// 同时有 checkpoint 和 CanFastTask stage（HIL 中断的典型状态）
	mem.SaveCheckpoint("s1", &react.Checkpoint{
		Messages: []react.Message{{Role: "user", Content: "继续"}},
		Step:     1,
		Query:    "原 query",
	})
	mem.SetPipeline("s1", "mix_video")
	mem.SetStage("s1", memory.FlowStageScriptConfirmed)
	mem.SetAgent("s1", "gold_scriptwriter")

	taskRunRepo := repository.NewTaskRunRepository(db)
	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{TaskRunRepo: taskRunRepo, StreamQ: &stubStream{}, Logger: zap.NewNop()}),
		TaskRunRepo: taskRunRepo,
		Logger:      zap.NewNop(),
	})

	handled, err := fp.CheckFastTask(ctx, &fastpath.FastTaskRequest{
		SessionID: "s1", UserID: 1, AgentID: "manager", Query: "继续", Resume: true,
	})
	testx.NoError(t, err)
	testx.True(t, handled, "resume 应优先命中")

	// 不应投递新 task_run（resume 不投递）
	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 0, "resume 不应投递新 task_run")
}

func TestFastPath_Retry_NotFoundIsNoOp(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()

	// 记忆里的失败 id 指向一条不存在的 task_run（如已被清理）
	mem.RecordFailedTaskRun("s1", model.NextID())

	taskRunRepo := repository.NewTaskRunRepository(db)
	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{TaskRunRepo: taskRunRepo, StreamQ: &stubStream{}, Logger: zap.NewNop()}),
		TaskRunRepo: taskRunRepo,
		Logger:      zap.NewNop(),
	})

	handled, err := fp.CheckFastTask(ctx, &fastpath.FastTaskRequest{
		SessionID:   "s1",
		UserID:      1,
		AgentID:     "manager",
		Query:       "重试",
		RequestType: "retry",
	})
	testx.NoError(t, err, "NotFound 应静默 no-op，不报错")
	testx.False(t, handled, "失败 task_run 不存在 → 不命中")

	// 不应投递任何新 task_run
	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 0, "NotFound 不应投递新 task_run")
}

// TestFastPath_ScriptContextPassThrough 验证带 OriginMsgID 时：
// 用 OriginMsgID 追溯脚本 task_run → 取 step_output → 拼进 edit_master 的 input。
func TestFastPath_ScriptContextPassThrough(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	testx.NoError(t, db.AutoMigrate(&model.CGTaskStep{}))
	mem := memory.NewIntentMemory()
	taskRunRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)

	// 预置脚本 task_run（MsgID = OriginMsgID）+ 脚本 step_output
	scriptRunID := model.NextID()
	scriptRun := &model.CGTaskRun{
		TaskRunID:     scriptRunID,
		UserID:        1,
		AgentID:       "gold_scriptwriter",
		SessionID:     "s1",
		OriginalQuery: "写脚本",
		MsgID:         "msg-script-1",
		Status:        model.TaskRunStatusFinished,
	}
	testx.NoError(t, taskRunRepo.Insert(ctx, scriptRun))
	testx.NoError(t, db.Create(&model.CGTaskStep{
		TaskStepID:   model.NextID(),
		TaskRunID:    scriptRunID,
		ParentStepID: 0,
		UserID:       1,
		AgentID:      "gold_scriptwriter",
		SessionID:    "s1",
		StepName:     "gold_scriptwriter",
		Status:       model.TaskStepStatusFinished,
		StepOutput:   `{"final_product":"【脚本】夏日旅行60秒混剪"}`,
	}).Error)

	// script_confirmed 命中 → 带 OriginMsgID
	mem.SetPipeline("s1", "mix_video")
	mem.SetStage("s1", memory.FlowStageScriptConfirmed)
	mem.SetAgent("s1", "gold_scriptwriter")

	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{TaskRunRepo: taskRunRepo, StreamQ: &stubStream{}, Logger: zap.NewNop()}),
		TaskRunRepo: taskRunRepo,
		StepRepo:    stepRepo,
		Logger:      zap.NewNop(),
	})

	handled, err := fp.CheckFastTask(ctx, &fastpath.FastTaskRequest{
		SessionID:   "s1",
		UserID:      1,
		AgentID:     "manager",
		Query:       "脚本已确认，请执行",
		OriginMsgID: "msg-script-1",
	})
	testx.NoError(t, err)
	testx.True(t, handled, "带 OriginMsgID + 脚本可解析 → 应命中快任务")

	// 投递的 edit_master input 应含脚本内容 + 用户确认语
	// （表里还有预置的脚本 task_run，过滤出本次投递的那条）
	var runs []model.CGTaskRun
	db.Find(&runs)
	var dispatched *model.CGTaskRun
	for i := range runs {
		if runs[i].MsgID == "" { // 脚本 task_run 有 MsgID=msg-script-1，本次投递无 MsgID
			dispatched = &runs[i]
			break
		}
	}
	testx.NotNil(t, dispatched, "应投递一个新的 edit_master task_run")
	plan, err := dispatched.ParsePlanSteps()
	testx.NoError(t, err)
	testx.Len(t, plan.Agents, 1)
	testx.Equal(t, "edit_master", plan.Agents[0].AgentID)
	testx.Contains(t, plan.Agents[0].Input, "【脚本】夏日旅行60秒混剪")
	testx.Contains(t, plan.Agents[0].Input, "脚本已确认，请执行")
	testx.True(t, strings.HasPrefix(plan.Agents[0].Input, "【脚本】"),
		"脚本内容应置于 input 前部")
}

// TestFastPath_ScriptContextMissing 验证带 OriginMsgID 但脚本查不到时：
// 不投递（对齐真实系统 judge.go skip 语义），交回 ReAct。
func TestFastPath_ScriptContextMissing(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()
	taskRunRepo := repository.NewTaskRunRepository(db)

	mem.SetPipeline("s1", "mix_video")
	mem.SetStage("s1", memory.FlowStageScriptConfirmed)
	mem.SetAgent("s1", "gold_scriptwriter")

	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{TaskRunRepo: taskRunRepo, StreamQ: &stubStream{}, Logger: zap.NewNop()}),
		TaskRunRepo: taskRunRepo,
		Logger:      zap.NewNop(),
	})

	handled, err := fp.CheckFastTask(ctx, &fastpath.FastTaskRequest{
		SessionID:   "s1",
		UserID:      1,
		AgentID:     "manager",
		Query:       "继续",
		OriginMsgID: "msg-not-exist",
	})
	testx.NoError(t, err)
	testx.False(t, handled, "OriginMsgID 脚本查不到 → 不命中，交回 ReAct")

	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 0, "脚本缺失不应投递新 task_run")
}
