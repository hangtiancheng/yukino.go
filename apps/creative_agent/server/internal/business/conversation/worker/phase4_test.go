package worker_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/fastpath"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/worker"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/executor"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"go.uber.org/zap"
)

// TestPhase4_FastTaskChain 验证点卡后快任务链路（Phase 4 4.3 milestone）：
// script_confirmed → FastPath 命中（BuildExecuteAgent 算下一个 agent=edit_master）→
// Submitter 投递 → task Consumer 消费执行 → Poller 回流 → task_run Finished。
func TestPhase4_FastTaskChain(t *testing.T) {
	ctx := context.Background()
	db := newPhase3TestDB(t)
	zlog, _ := zap.NewDevelopment()

	taskRunRepo := repository.NewTaskRunRepository(db)
	taskStepRepo := repository.NewTaskStepRepository(db)
	asyncRepo := repository.NewAsyncRequestRepository(db)
	msgRepo := repository.NewMessageRecordRepository(db)

	convStream := infra.NewMemoryStreamQueue("cg:stream:conversation", "cg:workers", "w1")
	taskStream := infra.NewMemoryStreamQueue("cg:stream:task", "cg:executors", "e1")
	chunkStore := infra.NewMemoryChunkStore()

	mem := memory.NewIntentMemory()

	submitter := subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
		TaskRunRepo: taskRunRepo,
		StreamQ:     taskStream,
		Logger:      zlog,
	})
	taskExec := subtaskexecutor.NewTaskExecutor(subtaskexecutor.TaskExecutorConfig{
		TaskRunRepo: taskRunRepo,
		StepRepo:    taskStepRepo,
		Tools:       appkittools.NewSubAgentToolRegistry(),
		Logger:      zlog,
	})
	taskConsumer := taskflow.NewConsumer(taskflow.ConsumerConfig{
		StreamQ:     taskStream,
		TaskRunRepo: taskRunRepo,
		Executor:    taskExec,
		ExecutorID:  "e1",
		Logger:      zlog,
	})
	poller := taskflow.NewPoller(taskflow.PollerConfig{
		StepRepo:    taskStepRepo,
		TaskRunRepo: taskRunRepo,
		Logger:      zlog,
	})

	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   submitter,
		TaskRunRepo: taskRunRepo,
		Logger:      zlog,
	})

	convConsumer := worker.NewConsumer(worker.ConsumerConfig{
		StreamQ:       convStream,
		ChunkStore:    chunkStore,
		Lock:          infra.NewMemoryLocker(),
		AsyncRepo:     asyncRepo,
		MsgRepo:       msgRepo,
		WorkerID:      "w1",
		Logger:        zlog,
		TaskSubmitter: submitter,
		TaskPoller:    poller,
		TaskRunRepo:   taskRunRepo,
		// 业务决策经 hooks 注入（对齐 app 装配；等价原 consumer 硬编码接线）。
		Hooks: &worker.TaskRunnerHooks{
			FastPath: fp,
			Memory:   mem,
		},
	})
	convCtx, convCancel := context.WithCancel(ctx)
	defer convCancel()
	go convConsumer.Start(convCtx)
	taskCtx, taskCancel := context.WithCancel(ctx)
	defer taskCancel()
	go taskConsumer.Start(taskCtx)

	// 预置 script_confirmed 状态（模拟用户已点脚本卡）
	mem.SetPipeline("s4", "mix_video")
	mem.SetAgent("s4", "gold_scriptwriter")
	mem.SetStage("s4", memory.FlowStageScriptConfirmed)

	// 发请求
	msgID := "msg-phase4"
	testx.NoError(t, asyncRepo.InsertPending(ctx, &model.CGAsyncRequest{
		AsyncRequestID: "ar-phase4",
		MessageID:      msgID,
		UserID:         1,
		SessionID:      "s4",
		AgentID:        "manager",
		Query:          "继续",
		Status:         model.AsyncStatusPending,
	}))
	ct := model.NewChatText("继续")
	ctJSON, _ := json.Marshal(ct)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: msgID, UserID: 1, AgentID: "manager", SessionID: "s4",
		ChatText: string(ctJSON), Status: model.MessageStatusInProgress,
	}))
	convBody, _ := json.Marshal(map[string]interface{}{
		"async_request_id": "ar-phase4",
		"msg_id":           msgID,
		"user_id":          1,
		"session_id":       "s4",
		"agent_id":         "manager",
		"query":            "继续",
	})
	testx.NoError(t, convStream.Push(ctx, msgID, convBody))

	// 快任务投递 edit_master → 子任务执行完成
	testx.Eventually(t, func() bool {
		var runs []model.CGTaskRun
		db.Where("session_id = ?", "s4").Find(&runs)
		for _, r := range runs {
			plan, _ := r.ParsePlanSteps()
			if len(plan.Agents) > 0 && plan.Agents[0].AgentID == "edit_master" &&
				r.Status == model.TaskRunStatusFinished {
				return true
			}
		}
		return false
	}, 15*time.Second, 100*time.Millisecond, "edit_master 子任务应执行完成")

	// 快任务链路不应走管家 ReAct（无 new_agents_plan 的 task_run）
	var runs []model.CGTaskRun
	db.Where("session_id = ?", "s4").Find(&runs)
	testx.Len(t, runs, 1, "快任务只投递一个 task_run")

	// task_step 已落库（agent step + tool step），task_run Finished
	var steps []model.CGTaskStep
	db.Where("task_run_id = ?", runs[0].TaskRunID).Find(&steps)
	testx.NotEmpty(t, steps, "应有子任务步骤")
	testx.Contains(t, steps[0].StepOutput, "final_product")

	// 异步请求终态 Completed（worker 的 fastTaskPollAndFinish 轮询完子任务后才 MarkTerminal）
	testx.Eventually(t, func() bool {
		req, err := asyncRepo.GetByMessageID(ctx, msgID)
		return err == nil && req != nil && req.Status == model.AsyncStatusCompleted
	}, 15*time.Second, 100*time.Millisecond, "异步请求应 Completed")

	// chunk 应有 EndTurn（回流闭环）
	testx.Eventually(t, func() bool {
		chunks, _, _, err := chunkStore.After(ctx, msgID, 0)
		testx.NoError(t, err)
		for _, c := range chunks {
			if c.EndTurn {
				return true
			}
		}
		return false
	}, 15*time.Second, 100*time.Millisecond, "应有 EndTurn chunk")

	// 快任务回流也应含 task-step / task-done 进度帧（与 Phase 3 管家 plan 路径一致的进度 UX）
	chunks, _, _, err := chunkStore.After(ctx, msgID, 0)
	testx.NoError(t, err)
	var types []string
	for _, c := range chunks {
		var f struct {
			Type string `json:"type"`
		}
		if c.EndTurn {
			types = append(types, "ENDTURN")
			continue
		}
		if err := json.Unmarshal([]byte(c.Payload), &f); err == nil && f.Type != "" {
			types = append(types, f.Type)
		}
	}
	testx.Contains(t, types, "task-step", "快任务应回流 task-step 进度帧")
	testx.Contains(t, types, "task-done", "快任务应回流 task-done 终态帧")
	testx.Contains(t, types, "message-complete")
}

// TestPhase4_HILInterruptAndResume 验证 HIL 完整闭环：
// ① query 含"确认" → Worker 注入 FakeHILStrategy（taskSubmitter!=nil 块内）→
//
//	管家 ReAct 产 Interrupt → SetOnInterrupt 保存 checkpoint
//
// ② resume 请求 → FastPath resume 分支命中 → runReActWithCheckpoint 恢复 →
//
//	checkpoint 清除 + 异步请求 Completed。
//
// 关键：FakeHILStrategy 注入在 `if c.taskSubmitter != nil` 块内（consumer.go:251），
// 因此 HIL 测试也必须配 Submitter——否则走 runner.Run（FakeLLM 不产 Interrupt）。
func TestPhase4_HILInterruptAndResume(t *testing.T) {
	ctx := context.Background()
	db := newPhase3TestDB(t)
	zlog := zap.NewNop()

	taskRunRepo := repository.NewTaskRunRepository(db)
	asyncRepo := repository.NewAsyncRequestRepository(db)
	msgRepo := repository.NewMessageRecordRepository(db)

	convStream := infra.NewMemoryStreamQueue("cg:stream:conversation", "cg:workers", "w1")
	chunkStore := infra.NewMemoryChunkStore()

	mem := memory.NewIntentMemory()

	// HIL 场景不投递子任务，但 FakeHILStrategy 注入在 taskSubmitter!=nil 块内，
	// 必须配一个真实 Submitter（走 RunWithOutput）。中断时不产 plan，投递无副作用。
	submitter := subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
		TaskRunRepo: taskRunRepo,
		StreamQ:     infra.NewMemoryStreamQueue("cg:stream:task", "cg:executors", "e1"),
		Logger:      zlog,
	})

	fp := fastpath.NewFastPath(fastpath.FastPathConfig{
		Memory:      mem,
		Submitter:   submitter,
		TaskRunRepo: taskRunRepo,
		Logger:      zlog,
	})

	convConsumer := worker.NewConsumer(worker.ConsumerConfig{
		StreamQ:       convStream,
		ChunkStore:    chunkStore,
		Lock:          infra.NewMemoryLocker(),
		AsyncRepo:     asyncRepo,
		MsgRepo:       msgRepo,
		WorkerID:      "w1",
		Logger:        zlog,
		TaskSubmitter: submitter,
		TaskRunRepo:   taskRunRepo,
		// 业务决策经 hooks 注入（对齐 app 装配；等价原 consumer 硬编码接线）。
		Hooks: &worker.TaskRunnerHooks{
			FastPath: fp,
			Memory:   mem,
			// HIL 触发词与 app 装配一致（query 含"确认" → 启用 FakeHILStrategy）。
			ShouldEnableHIL: func(query string) bool {
				return strings.Contains(query, "确认")
			},
		},
	})
	convCtx, convCancel := context.WithCancel(ctx)
	defer convCancel()
	go convConsumer.Start(convCtx)

	// ① 发含"确认"的请求（触发 HIL 中断）
	msgID := "msg-hil"
	testx.NoError(t, asyncRepo.InsertPending(ctx, &model.CGAsyncRequest{
		AsyncRequestID: "ar-hil",
		MessageID:      msgID,
		UserID:         1,
		SessionID:      "s-hil",
		AgentID:        "manager",
		Query:          "生成一个脚本，需要你确认一下方案",
		Status:         model.AsyncStatusPending,
	}))
	ct := model.NewChatText("生成一个脚本，需要你确认一下方案")
	ctJSON, _ := json.Marshal(ct)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: msgID, UserID: 1, AgentID: "manager", SessionID: "s-hil",
		ChatText: string(ctJSON), Status: model.MessageStatusInProgress,
	}))
	convBody, _ := json.Marshal(map[string]interface{}{
		"async_request_id": "ar-hil",
		"msg_id":           msgID,
		"user_id":          1,
		"session_id":       "s-hil",
		"agent_id":         "manager",
		"query":            "生成一个脚本，需要你确认一下方案",
	})
	testx.NoError(t, convStream.Push(ctx, msgID, convBody))

	// 等待 checkpoint 保存（中断发生）
	testx.Eventually(t, func() bool {
		return mem.LoadCheckpoint("s-hil") != nil
	}, 15*time.Second, 100*time.Millisecond, "HIL 中断应保存 checkpoint")

	// 中断帧已回流（interrupt 帧 + EndTurn）
	testx.Eventually(t, func() bool {
		chunks, _, _, err := chunkStore.After(ctx, msgID, 0)
		testx.NoError(t, err)
		hasInterrupt, hasEnd := false, false
		for _, c := range chunks {
			var f struct {
				Type string `json:"type"`
			}
			if c.EndTurn {
				hasEnd = true
				continue
			}
			if err := json.Unmarshal([]byte(c.Payload), &f); err == nil && f.Type == "interrupt" {
				hasInterrupt = true
			}
		}
		return hasInterrupt && hasEnd
	}, 15*time.Second, 100*time.Millisecond, "应回流 interrupt 帧 + EndTurn")

	// ② 发 resume 请求
	msgID2 := "msg-hil-resume"
	testx.NoError(t, asyncRepo.InsertPending(ctx, &model.CGAsyncRequest{
		AsyncRequestID: "ar-hil-resume",
		MessageID:      msgID2,
		UserID:         1,
		SessionID:      "s-hil",
		AgentID:        "manager",
		Query:          "继续",
		Status:         model.AsyncStatusPending,
	}))
	ct2 := model.NewChatText("继续")
	ct2JSON, _ := json.Marshal(ct2)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: msgID2, UserID: 1, AgentID: "manager", SessionID: "s-hil",
		ChatText: string(ct2JSON), Status: model.MessageStatusInProgress,
	}))
	resumeBody, _ := json.Marshal(map[string]interface{}{
		"async_request_id": "ar-hil-resume",
		"msg_id":           msgID2,
		"user_id":          1,
		"session_id":       "s-hil",
		"agent_id":         "manager",
		"query":            "继续",
		"resume":           true,
	})
	testx.NoError(t, convStream.Push(ctx, msgID2, resumeBody))

	// 等待 resume 完成 → 异步请求 Completed
	testx.Eventually(t, func() bool {
		req, _ := asyncRepo.GetByMessageID(ctx, msgID2)
		return req != nil && req.Status == model.AsyncStatusCompleted
	}, 15*time.Second, 100*time.Millisecond, "resume 应完成")
	testx.Eventually(t, func() bool {
		return mem.LoadCheckpoint("s-hil") == nil
	}, 15*time.Second, 100*time.Millisecond, "resume 后 checkpoint 应清除")
}
