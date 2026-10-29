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

// newFastPathDB creates an in-memory sqlite DB with the cg_task_run table registered.
func newFastPathDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGTaskRun{}))
	return db
}

// stubStream is the in-memory StreamQ stub for tests (implements the infra.StreamQueue interface).
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

	// In-memory stream stub
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

	// script_confirmed hit → handled + dispatched
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
	testx.True(t, handled, "script_confirmed should hit the fast task")

	// A new execute_agent task_run was dispatched (next in the chain = edit_master)
	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 1, "one task_run should be dispatched")
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
	testx.False(t, handled, "FlowStageEmpty should not hit")
}

func TestFastPath_Retry(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()

	// Pre-create a failed task_run (simulating the last failure)
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
	testx.True(t, handled, "a retry request should hit")

	// A new task_run was dispatched (inheriting the failed run's plan)
	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 2, "there should be one new task_run")
}

func TestFastPath_HILResume(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()

	// Pre-set the checkpoint
	cp := &react.Checkpoint{
		Messages: []react.Message{{Role: "user", Content: "继续"}},
		Step:     1,
		Query:    "原 query",
	}
	mem.SaveCheckpoint("s1", cp)

	taskRunRepo := repository.NewTaskRunRepository(db)
	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   nil, // resume does not dispatch; Submitter may be nil
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
	testx.True(t, handled, "resume with an existing checkpoint should hit")
}

func TestFastPath_ResumePrecedence(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()

	// Both a checkpoint and a CanFastTask stage exist (the typical HIL-interrupt state)
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
	testx.True(t, handled, "resume should hit first")

	// No new task_run should be dispatched (resume does not dispatch)
	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 0, "resume should not dispatch a new task_run")
}

func TestFastPath_Retry_NotFoundIsNoOp(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()

	// The remembered failed id points to a nonexistent task_run (e.g. cleaned up)
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
	testx.NoError(t, err, "NotFound should silently no-op without error")
	testx.False(t, handled, "the failed task_run does not exist → no hit")

	// No new task_run should be dispatched
	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 0, "NotFound should not dispatch a new task_run")
}

// TestFastPath_ScriptContextPassThrough verifies that with OriginMsgID:
// the script task_run is traced by OriginMsgID → step_output is taken → spliced into edit_master's input.
func TestFastPath_ScriptContextPassThrough(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	testx.NoError(t, db.AutoMigrate(&model.CGTaskStep{}))
	mem := memory.NewIntentMemory()
	taskRunRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)

	// Pre-create the script task_run (MsgID = OriginMsgID) + the script step_output
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

	// script_confirmed hit → with OriginMsgID
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
	testx.True(t, handled, "OriginMsgID + a parsable script → the fast task should hit")

	// The dispatched edit_master input should contain the script content + the user's confirmation text
	// (the table also holds the pre-created script task_run; filter out this dispatch's row)
	var runs []model.CGTaskRun
	db.Find(&runs)
	var dispatched *model.CGTaskRun
	for i := range runs {
		if runs[i].MsgID == "" { // the script task_run has MsgID=msg-script-1; this dispatch has no MsgID
			dispatched = &runs[i]
			break
		}
	}
	testx.NotNil(t, dispatched, "a new edit_master task_run should be dispatched")
	plan, err := dispatched.ParsePlanSteps()
	testx.NoError(t, err)
	testx.Len(t, plan.Agents, 1)
	testx.Equal(t, "edit_master", plan.Agents[0].AgentID)
	testx.Contains(t, plan.Agents[0].Input, "【脚本】夏日旅行60秒混剪")
	testx.Contains(t, plan.Agents[0].Input, "脚本已确认，请执行")
	testx.True(t, strings.HasPrefix(plan.Agents[0].Input, "【脚本】"),
		"the script content should be placed at the front of the input")
}

// TestFastPath_ScriptContextMissing verifies that with OriginMsgID but no findable script:
// nothing is dispatched (aligned with the reference system's judge.go skip semantics); control returns to ReAct.
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
	testx.False(t, handled, "OriginMsgID script not found → no hit; hand back to ReAct")

	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 0, "a missing script should not dispatch a new task_run")
}

// TestFastPath_ChainEndsAfterDispatch verifies that a fast-task dispatch advances the session's
// agent memory, so a subsequent request in the same *_confirmed stage does NOT re-dispatch the same
// agent. mix_video's chain is idea_king → gold_scriptwriter → edit_master; after script_confirmed
// dispatches edit_master (the last agent), the next hit must reach the chain end and hand back to ReAct.
func TestFastPath_ChainEndsAfterDispatch(t *testing.T) {
	ctx := context.Background()
	db := newFastPathDB(t)
	mem := memory.NewIntentMemory()
	taskRunRepo := repository.NewTaskRunRepository(db)

	submitter := subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
		TaskRunRepo: taskRunRepo, StreamQ: &stubStream{}, Logger: zap.NewNop(),
	})
	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   submitter,
		TaskRunRepo: taskRunRepo,
		Logger:      zap.NewNop(),
	})

	mem.SetPipeline("s1", "mix_video")
	mem.SetStage("s1", memory.FlowStageScriptConfirmed)
	mem.SetAgent("s1", "gold_scriptwriter")

	req := &fastpath.FastTaskRequest{SessionID: "s1", UserID: 1, AgentID: "manager", Query: "继续执行视频合成"}

	// First hit: dispatches edit_master and advances the agent memory to it.
	handled, err := fp.CheckFastTask(ctx, req)
	testx.NoError(t, err)
	testx.True(t, handled, "script_confirmed should dispatch the next agent")
	testx.Equal(t, "edit_master", mem.GetAgent("s1"), "the dispatched agent should be recorded")

	// Second hit (e.g. the user sends another message while the stage is still script_confirmed):
	// edit_master is the last agent → chain end → no re-dispatch, hand back to ReAct.
	handled2, err := fp.CheckFastTask(ctx, req)
	testx.NoError(t, err)
	testx.False(t, handled2, "chain ended at edit_master; must not re-dispatch")

	var runs []model.CGTaskRun
	db.Find(&runs)
	testx.Len(t, runs, 1, "exactly one task_run should be dispatched across both hits")
}
