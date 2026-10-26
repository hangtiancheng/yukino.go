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

// newExecutorDB 创建内存 sqlite，注册 task_run + task_step。
// MaxOpenConns(1)：sqlite :memory: 的每个连接是独立空库，必须限制单连接，
// 否则并发读写（如 PollUntilDone 测试）会读到不同的空库。
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

// newTaskStream 创建子任务 stream 的 StreamQueue（进程内实现，替代 miniredis）。
func newTaskStream(t *testing.T) infra.StreamQueue {
	t.Helper()
	return infra.NewMemoryStreamQueue("cg:stream:task", "cg:executors", "exec-test")
}

// TestSubmitter_Submit_Then_Consumer_Preempt 验证 Submitter 先建后投 + Consumer 消费抢占。
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

	// 构造一个 plan_steps（含 Agents[0]）
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

	// Submit：先建 task_run 再投递
	taskRunID, err := submitter.Submit(ctx, run)
	testx.NoError(t, err)
	testx.NotZero(t, taskRunID)

	// 验证 task_run 已落库为 Pending
	got, err := taskRunRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusPending, got.Status)
	testx.Contains(t, got.PlanSteps, "gold_scriptwriter")

	// Consumer（未接线 TaskExecutor → 只做抢占验证）
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

	// 等待抢占完成（轮询 task_run 状态翻为终态）
	testx.Eventually(t, func() bool {
		r, err := taskRunRepo.GetByTaskRunID(ctx, taskRunID)
		if err != nil {
			return false
		}
		return r.Status != model.TaskRunStatusPending
	}, 10*time.Second, 50*time.Millisecond, "consumer 应抢占并终态化 task_run")

	cancel()
	<-done

	// 无 executor 时直接 MarkTerminal(Finished)
	fin, err := taskRunRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFinished, fin.Status)
}

// TestTaskExecutor_SubAgent_NeedWaitNo 验证 NeedWait=No：子智能体产出终态 → Finished。
func TestTaskExecutor_SubAgent_NeedWaitNo(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)
	stepRepo := repository.NewTaskStepRepository(db)
	tools := appkittools.NewSubAgentToolRegistry()

	// 建 task_run（直接 Insert，跳过 Submitter）
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

	// agent step → Finished + step_output 含 final_product
	steps, err := stepRepo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.NotEmpty(t, steps)

	agentStep := steps[0]
	testx.Equal(t, model.TaskStepStatusFinished, agentStep.Status)
	testx.Contains(t, agentStep.StepOutput, "final_product")
	testx.Contains(t, agentStep.StepOutput, "img.example.com")
}

// TestTaskExecutor_SubAgent_NeedWaitYes 验证 NeedWait=Yes：
// 产线工具（create_video_task）→ task_run 置 Waiting，intermediate_result 已写。
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

	// 注入产线 LLM：第一轮 create_video_task（isPipeline=true）
	exec := subtaskexecutor.NewTaskExecutor(subtaskexecutor.TaskExecutorConfig{
		TaskRunRepo: taskRunRepo,
		StepRepo:    stepRepo,
		Tools:       tools,
		LLM:         &appkitfake.FakeSubLLMPipeline{},
		Logger:      zlog,
	})

	shouldAck := exec.Execute(ctx, run)
	testx.True(t, shouldAck)

	// task_run → Waiting（不是 Finished，等产线回调）
	got, err := taskRunRepo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusWaiting, got.Status)

	// agent step → need_wait=Yes + intermediate_result 已写（pipeline_task_id）
	steps, err := stepRepo.GetByTaskRunID(ctx, run.TaskRunID)
	testx.NoError(t, err)
	testx.NotEmpty(t, steps)
	testx.Equal(t, model.NeedWaitYes, steps[0].NeedWait)
	testx.Contains(t, steps[0].IntermediateResult, "pipeline_task_id")
}

// TestSubmitter_Submit_PushFailure_MarksFailed 验证投递失败不阻断：
// 即使 stream Push 失败，task_run 也已建成并标记 Failed，Submit 不返回 error。
func TestSubmitter_Submit_PushFailure_MarksFailed(t *testing.T) {
	db := newExecutorDB(t)
	ctx := context.Background()
	zlog := logger.New("info")

	taskRunRepo := repository.NewTaskRunRepository(db)

	// 用一个永远失败的 StreamQueue
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
	testx.NoError(t, err, "投递失败不阻断，Submit 仍返回成功")
	testx.NotZero(t, taskRunID)

	got, err := taskRunRepo.GetByTaskRunID(ctx, taskRunID)
	testx.NoError(t, err)
	testx.Equal(t, model.TaskRunStatusFailed, got.Status, "投递失败应标记 Failed")
}

// failingStreamQueue 是永远 Push 失败的 StreamQueue 桩。
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

// TestFlowStage_CanFastTask 验证 FlowStage 的 CanFastTask 判断。
func TestFlowStage_CanFastTask(t *testing.T) {
	// 未推进 / 早期确认阶段 → 不触发快任务
	testx.False(t, memory.FlowStageEmpty.CanFastTask())
	testx.False(t, memory.FlowStageTopicConfirmed.CanFastTask())
	testx.False(t, memory.FlowStageCategoryConfirmed.CanFastTask())

	// 参数/策划/脚本确认 → 触发快任务
	testx.True(t, memory.FlowStageParamsConfirmed.CanFastTask())
	testx.True(t, memory.FlowStagePlanConfirmed.CanFastTask())
	testx.True(t, memory.FlowStageScriptConfirmed.CanFastTask())
}

// TestIntentMemory 验证意图记忆的读写 + 快照。
func TestIntentMemory(t *testing.T) {
	m := memory.NewIntentMemory()

	// 初始为空
	testx.Equal(t, memory.FlowStageEmpty, m.GetStage("s1"))

	// 推进
	m.SetPipeline("s1", string(subtask.PipelineMixVideo))
	m.SetAgent("s1", string(subtask.AgentGoldScriptwriter))
	m.SetStage("s1", memory.FlowStageScriptConfirmed)

	testx.Equal(t, memory.FlowStageScriptConfirmed, m.GetStage("s1"))
	testx.Equal(t, string(subtask.PipelineMixVideo), m.GetPipeline("s1"))
	testx.Equal(t, string(subtask.AgentGoldScriptwriter), m.GetAgent("s1"))

	// 快照
	snap := m.Snapshot("s1")
	testx.Equal(t, "script_confirmed", snap["stage"])
	testx.Equal(t, "mix_video", snap["pipeline"])

	// 不同 session 隔离
	testx.Equal(t, memory.FlowStageEmpty, m.GetStage("s2"))
}

// TestBuildExecuteAgent 验证脚本确认后构造下一个 agent 的 plan（依赖 subtask 的 GetNextAgent）。
func TestBuildExecuteAgent(t *testing.T) {
	agents, err := subtaskexecutor.BuildExecuteAgent(
		subtask.PipelineMixVideo,
		subtask.AgentGoldScriptwriter,
		1, "s1", "gold_scriptwriter", "生成一段混剪视频", "",
	)
	testx.NoError(t, err)
	testx.Len(t, agents, 1, "每次只投递单个 agent")
	testx.Equal(t, string(subtask.AgentEditMaster), agents[0].AgentID)
	testx.Equal(t, "生成一段混剪视频", agents[0].Input)

	// 已是最后一个 → 链结束，返回 nil
	agents2, err := subtaskexecutor.BuildExecuteAgent(
		subtask.PipelineMixVideo,
		subtask.AgentEditMaster,
		1, "s1", "edit_master", "q", "",
	)
	testx.NoError(t, err)
	testx.Nil(t, agents2)

	// 前序脚本非空 → 拼进 input（脚本置前，用户确认语置后）
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
		"脚本内容应置于 input 前部")
}
