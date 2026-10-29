package subtaskexecutor_test

import (
	"context"
	"strings"
	"testing"
	"time"

	appkitfake "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/fake"
	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/executor"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/logger"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newExecutorDB creates an in-memory sqlite DB with task_run + task_step registered.
// MaxOpenConns(1): each connection to sqlite :memory: is an independent empty database; a single connection must be enforced,
// otherwise concurrent reads/writes (e.g. the PollUntilDone test) would hit different empty databases.
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

// newTaskStream creates the StreamQueue of the subtask stream (in-process implementation, replacing miniredis).
func newTaskStream(t *testing.T) infra.StreamQueue {
	t.Helper()
	return infra.NewMemoryStreamQueue("cg:stream:task", "cg:executors", "exec-test")
}

// TestSubmitter_Submit_Then_Consumer_Preempt verifies the Submitter's create-then-dispatch + the Consumer's consumption claim.
func TestSubmitter_Submit_Then_Consumer_Preempt(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)
	streamQ := newTaskStream(t)

	submitter := subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
		TaskRunRepo: taskRunRepo,
		StreamQ:     streamQ,
		Logger:      zlog,
	})

	// Build a plan_steps (containing Agents[0])
	run := &model.CGTaskRun{
		UserID:        1,
		AgentID:       "gold_scriptwriter",
		SessionID:     "s1",
		OriginalQuery: "生成一张海报",
		Status:        model.TaskRunStatusPending,
	}
	plan := &model.PlanSteps{
		Agents: []model.PlanAgent{{AgentID: "gold_scriptwriter", Input: "写一份海报文案"}},
	}
	_ = run.SetPlanSteps(plan)

	// Submit: create task_run first, then dispatch
	taskRunID, err := submitter.Submit(ctx, run)
	testx.NoError(t, err)
	testx.NotZero(t, taskRunID)

	// Verify task_run was persisted as Pending
	got, err := taskRunRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusPending, got.Status)
	testx.Contains(t, got.PlanSteps, "gold_scriptwriter")

	// Consumer (no TaskExecutor wired → only validates the claim)
	con := taskflow.NewConsumer(taskflow.ConsumerConfig{
		StreamQ:     streamQ,
		TaskRunRepo: taskRunRepo,
		ExecutorID:  "exec-1",
		Logger:      zlog,
	})
	conCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		con.Start(conCtx)
	}()

	// Wait for the claim to complete (poll until task_run flips to terminal)
	testx.Eventually(t, func() bool {
		r, err := taskRunRepo.GetByTaskRunID(ctx, taskRunID)
		if err != nil {
			return false
		}
		return r.Status != model.TaskRunStatusPending
	}, 10*time.Second, 50*time.Millisecond, "the consumer should claim and terminate the task_run")

	cancel()
	<-done

	// Without an executor it MarkTerminal(Finished) directly
	fin, err := taskRunRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFinished, fin.Status)
}

// TestTaskExecutor_SubAgent_NeedWaitNo verifies NeedWait=No: the sub-agent produces the terminal state → Finished.
func TestTaskExecutor_SubAgent_NeedWaitNo(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)
	tools := appkittools.NewSubAgentToolRegistry()

	// Create task_run (direct Insert, skipping the Submitter)
	run := &model.CGTaskRun{
		TaskRunID:     model.NextID(),
		UserID:        1,
		AgentID:       "gold_scriptwriter",
		SessionID:     "s1",
		OriginalQuery: "生成一张海报",
		Status:        model.TaskRunStatusPending,
	}
	plan := &model.PlanSteps{
		Agents: []model.PlanAgent{{AgentID: "gold_scriptwriter", Input: "写一份海报文案"}},
	}
	_ = run.SetPlanSteps(plan)
	testx.NoError(t, taskRunRepo.Insert(ctx, run))

	exec := subtaskexecutor.NewTaskExecutor(subtaskexecutor.TaskExecutorConfig{
		TaskRunRepo: taskRunRepo,
		StepRepo:    stepRepo,
		Tools:       tools,
		Logger:      zlog,
	})

	shouldAck := exec.Execute(ctx, run)
	testx.True(t, shouldAck)

	// task_run → Finished
	got, err := taskRunRepo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFinished, got.Status)

	// agent step → Finished + step_output contains final_product
	steps, err := stepRepo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.NotEmpty(t, steps)

	agentStep := steps[0]
	testx.Equal(t, model.TaskStepStatusFinished, agentStep.Status)
	testx.Contains(t, agentStep.StepOutput, "final_product")
	testx.Contains(t, agentStep.StepOutput, "img.example.com")
}

// TestTaskExecutor_SubAgent_NeedWaitYes verifies NeedWait=Yes:
// pipeline tool (create_video_task) → task_run set to Waiting, intermediate_result written.
func TestTaskExecutor_SubAgent_NeedWaitYes(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)
	tools := appkittools.NewSubAgentToolRegistry()

	run := &model.CGTaskRun{
		TaskRunID:     model.NextID(),
		UserID:        1,
		AgentID:       "video_agent",
		SessionID:     "s1",
		OriginalQuery: "做一条视频",
		Status:        model.TaskRunStatusPending,
	}
	plan := &model.PlanSteps{
		Agents: []model.PlanAgent{{AgentID: "video_agent", Input: "生成城市夜景视频"}},
	}
	_ = run.SetPlanSteps(plan)
	testx.NoError(t, taskRunRepo.Insert(ctx, run))

	// Inject the pipeline LLM: first round create_video_task (isPipeline=true)
	exec := subtaskexecutor.NewTaskExecutor(subtaskexecutor.TaskExecutorConfig{
		TaskRunRepo: taskRunRepo,
		StepRepo:    stepRepo,
		Tools:       tools,
		LLM:         &appkitfake.FakeSubLLMPipeline{},
		Logger:      zlog,
	})

	shouldAck := exec.Execute(ctx, run)
	testx.True(t, shouldAck)

	// task_run → Waiting (not Finished; awaiting the pipeline callback)
	got, err := taskRunRepo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusWaiting, got.Status)

	// agent step → need_wait=Yes + intermediate_result written (pipeline_task_id)
	steps, err := stepRepo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.NotEmpty(t, steps)
	testx.Equal(t, model.NeedWaitYes, steps[0].NeedWait)
	testx.Contains(t, steps[0].IntermediateResult, "pipeline_task_id")
}

// TestSubmitter_Submit_PushFailure_MarksFailed verifies that dispatch failure does not block:
// even when the stream Push fails, task_run is created and marked Failed, and Submit returns no error.
func TestSubmitter_Submit_PushFailure_MarksFailed(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)

	// Use a StreamQueue that always fails
	failingQ := &failingStreamQueue{}

	submitter := subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
		TaskRunRepo: taskRunRepo,
		StreamQ:     failingQ,
		Logger:      zlog,
	})

	run := &model.CGTaskRun{
		UserID:        1,
		AgentID:       "gold_scriptwriter",
		SessionID:     "s1",
		OriginalQuery: "q",
		Status:        model.TaskRunStatusPending,
	}
	taskRunID, err := submitter.Submit(ctx, run)
	testx.NoError(t, err, "dispatch failure does not block; Submit still returns success")
	testx.NotZero(t, taskRunID)

	got, err := taskRunRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFailed, got.Status, "a dispatch failure should mark Failed")
}

// failingStreamQueue is a StreamQueue stub whose Push always fails.
type failingStreamQueue struct{}

func (f *failingStreamQueue) Push(ctx context.Context, msgID string, body []byte) error {
	return testx.AnError
}
func (f *failingStreamQueue) Receive(ctx context.Context) (*infra.StreamMessage, error) {
	return nil, nil
}
func (f *failingStreamQueue) Ack(ctx context.Context, streamMsgID string) error { return nil }
func (f *failingStreamQueue) Requeue(ctx context.Context, msgID string, body []byte) error {
	return nil
}
func (f *failingStreamQueue) ClaimPending(ctx context.Context, minIdle time.Duration) ([]*infra.StreamMessage, error) {
	return nil, nil
}

// TestFlowStage_CanFastTask verifies the FlowStage CanFastTask decision.
func TestFlowStage_CanFastTask(t *testing.T) {
	// Not advanced / early confirmation stages → no fast task triggered
	testx.False(t, memory.FlowStageEmpty.CanFastTask())
	testx.False(t, memory.FlowStageTopicConfirmed.CanFastTask())
	testx.False(t, memory.FlowStageCategoryConfirmed.CanFastTask())

	// Params/plan/script confirmed → fast task triggered
	testx.True(t, memory.FlowStageParamsConfirmed.CanFastTask())
	testx.True(t, memory.FlowStagePlanConfirmed.CanFastTask())
	testx.True(t, memory.FlowStageScriptConfirmed.CanFastTask())
}

// TestIntentMemory verifies intent-memory reads/writes + snapshots.
func TestIntentMemory(t *testing.T) {
	m := memory.NewIntentMemory()

	// Empty initially
	testx.Equal(t, memory.FlowStageEmpty, m.GetStage("s1"))

	// Advance
	m.SetPipeline("s1", string(subtask.PipelineMixVideo))
	m.SetAgent("s1", string(subtask.AgentGoldScriptwriter))
	m.SetStage("s1", memory.FlowStageScriptConfirmed)

	testx.Equal(t, memory.FlowStageScriptConfirmed, m.GetStage("s1"))
	testx.Equal(t, string(subtask.PipelineMixVideo), m.GetPipeline("s1"))
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), m.GetAgent("s1"))

	// Snapshot
	snap := m.Snapshot("s1")
	testx.Equal(t, "script_confirmed", snap["stage"])
	testx.Equal(t, "mix_video", snap["pipeline"])

	// Different sessions are isolated
	testx.Equal(t, memory.FlowStageEmpty, m.GetStage("s2"))
}

// TestBuildExecuteAgent verifies building the next agent's plan after script confirmation (depends on subtask's GetNextAgent).
func TestBuildExecuteAgent(t *testing.T) {
	agents, err := subtaskexecutor.BuildExecuteAgent(
		subtask.PipelineMixVideo,
		subtask.AgentGoldScriptwriter,
		1, "s1", "gold_scriptwriter", "生成一段混剪视频", "",
	)
	testx.NoError(t, err)
	testx.Len(t, agents, 1, "only a single agent is dispatched each time")
	testx.Equal(t, string(subtask.AgentEditMaster), agents[0].AgentID)
	testx.Equal(t, "生成一段混剪视频", agents[0].Input)

	// Already the last → chain end, returns nil
	agents2, err := subtaskexecutor.BuildExecuteAgent(
		subtask.PipelineMixVideo,
		subtask.AgentEditMaster,
		1, "s1", "edit_master", "q", "",
	)
	testx.NoError(t, err)
	testx.Nil(t, agents2)

	// Non-empty preceding script → spliced into input (script first, user confirmation text after)
	agents3, err := subtaskexecutor.BuildExecuteAgent(
		subtask.PipelineMixVideo,
		subtask.AgentGoldScriptwriter,
		1, "s1", "gold_scriptwriter", "脚本已确认，请执行", "【脚本】夏日旅行60秒混剪",
	)
	testx.NoError(t, err)
	testx.Len(t, agents3, 1)
	testx.Contains(t, agents3[0].Input, "【脚本】夏日旅行60秒混剪")
	testx.Contains(t, agents3[0].Input, "脚本已确认，请执行")
	testx.True(t, strings.HasPrefix(agents3[0].Input, "【脚本】"),
		"the script content should be placed at the front of the input")
}
