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

// TestPhase4_FastTaskChain verifies the post-card-click fast-task chain (Phase 4 4.3 milestone):
// script_confirmed → FastPath hit (BuildExecuteAgent computes the next agent=edit_master) →
// Submitter dispatches → task Consumer executes → Poller backflow → task_run Finished.
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
		// Business decisions are injected via hooks (aligned with the app wiring; equivalent to the original consumer's hard-coded wiring).
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

	// Pre-set the script_confirmed state (simulating the user having clicked the script card)
	mem.SetPipeline("s4", "mix_video")
	mem.SetAgent("s4", "gold_scriptwriter")
	mem.SetStage("s4", memory.FlowStageScriptConfirmed)

	// Send the request
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

	// The fast task dispatches edit_master → the subtask finishes
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
	}, 15*time.Second, 100*time.Millisecond, "the edit_master subtask should finish")

	// The fast-task chain must not run the manager ReAct (no new_agents_plan task_run)
	var runs []model.CGTaskRun
	db.Where("session_id = ?", "s4").Find(&runs)
	testx.Len(t, runs, 1, "the fast task dispatches exactly one task_run")

	// task_step persisted (agent step + tool step), task_run Finished
	var steps []model.CGTaskStep
	db.Where("task_run_id = ?", runs[0].TaskRunID).Find(&steps)
	testx.NotEmpty(t, steps, "subtask steps should exist")
	testx.Contains(t, steps[0].StepOutput, "final_product")

	// Async request terminal state Completed (the worker's fastTaskPollAndFinish MarkTerminals only after polling the subtask to completion)
	testx.Eventually(t, func() bool {
		req, err := asyncRepo.GetByMessageID(ctx, msgID)
		return err == nil && req != nil && req.Status == model.AsyncStatusCompleted
	}, 15*time.Second, 100*time.Millisecond, "the async request should be Completed")

	// Chunks should have EndTurn (closing the backflow loop)
	testx.Eventually(t, func() bool {
		chunks, _, _, err := chunkStore.After(ctx, msgID, 0)
		testx.NoError(t, err)
		for _, c := range chunks {
			if c.EndTurn {
				return true
			}
		}
		return false
	}, 15*time.Second, 100*time.Millisecond, "an EndTurn chunk should exist")

	// Fast-task backflow should also contain task-step / task-done progress frames (the same progress UX as the Phase 3 manager-plan path)
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
	testx.Contains(t, types, "task-step", "the fast task should flow back task-step progress frames")
	testx.Contains(t, types, "task-done", "the fast task should flow back the task-done terminal frame")
	testx.Contains(t, types, "message-complete")
}

// TestPhase4_HILInterruptAndResume verifies the full HIL loop:
// ① query contains "确认" → the Worker injects FakeHILStrategy (inside the taskSubmitter!=nil block) →
//
//	the manager ReAct produces Interrupt → SetOnInterrupt saves the checkpoint
//
// ② resume request → the FastPath resume branch hits → runReActWithCheckpoint restores →
//
//	the checkpoint is cleared + the async request Completed.
//
// Key point: FakeHILStrategy injection lives inside the `if c.taskSubmitter != nil` block (consumer.go:251),
// so HIL tests must also wire a Submitter — otherwise runner.Run is used (FakeLLM never produces Interrupt).
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

	// The HIL scenario dispatches no subtasks, but FakeHILStrategy injection lives inside the taskSubmitter!=nil block,
	// so a real Submitter must be wired (RunWithOutput path). No plan is produced on interrupt, so dispatching has no side effects.
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
		// Business decisions are injected via hooks (aligned with the app wiring; equivalent to the original consumer's hard-coded wiring).
		Hooks: &worker.TaskRunnerHooks{
			FastPath: fp,
			Memory:   mem,
			// The HIL trigger word matches the app wiring (query contains "确认" → enable FakeHILStrategy).
			ShouldEnableHIL: func(query string) bool {
				return strings.Contains(query, "确认")
			},
		},
	})
	convCtx, convCancel := context.WithCancel(ctx)
	defer convCancel()
	go convConsumer.Start(convCtx)

	// ① Send a request containing "确认" (triggering the HIL interrupt)
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

	// Wait for the checkpoint to be saved (the interrupt happened)
	testx.Eventually(t, func() bool {
		return mem.LoadCheckpoint("s-hil") != nil
	}, 15*time.Second, 100*time.Millisecond, "the HIL interrupt should save a checkpoint")

	// The interrupt frame flowed back (interrupt frame + EndTurn)
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
	}, 15*time.Second, 100*time.Millisecond, "the interrupt frame + EndTurn should flow back")

	// ② Send the resume request
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

	// Wait for the resume to complete → async request Completed
	testx.Eventually(t, func() bool {
		req, _ := asyncRepo.GetByMessageID(ctx, msgID2)
		return req != nil && req.Status == model.AsyncStatusCompleted
	}, 15*time.Second, 100*time.Millisecond, "the resume should complete")
	testx.Eventually(t, func() bool {
		return mem.LoadCheckpoint("s-hil") == nil
	}, 15*time.Second, 100*time.Millisecond, "the checkpoint should be cleared after resume")
}
