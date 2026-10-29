package worker_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/worker"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/executor"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/logger"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newPhase3TestDB creates an in-memory sqlite DB with all Phase 3 tables registered.
func newPhase3TestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(
		&model.CGAsyncRequest{},
		&model.CGMessageRecord{},
		&model.CGTaskRun{},
		&model.CGTaskStep{},
	))
	sqlDB, err := db.DB()
	testx.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	return db
}

// TestPhase3_EndToEnd_PlanToPoll verifies the full Phase 3 chain:
// the manager produces new_agents_plan → Submitter dispatches → Consumer consumes →
// TaskExecutor executes → Poller polls cg_task_step for backflow.
func TestPhase3_EndToEnd_PlanToPoll(t *testing.T) {
	ctx := context.Background()
	zlog := logger.New("info")
	db := newPhase3TestDB(t)

	asyncRepo := repository.NewAsyncRequestRepository(db)
	msgRepo := repository.NewMessageRecordRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	taskStepRepo := repository.NewTaskStepRepository(db)

	// Two streams: conversation + subtask
	convStream := infra.NewMemoryStreamQueue("cg:stream:conversation", "cg:workers", "w1")
	taskStream := infra.NewMemoryStreamQueue("cg:stream:task", "cg:executors", "e1")
	chunkStore := infra.NewMemoryChunkStore()
	defer chunkStore.Close()

	// Assemble the Phase 3 components
	tools := appkittools.NewSubAgentToolRegistry()
	taskExec := subtaskexecutor.NewTaskExecutor(subtaskexecutor.TaskExecutorConfig{
		TaskRunRepo: taskRunRepo,
		StepRepo:    taskStepRepo,
		Tools:       tools,
		Logger:      zlog,
	})
	taskConsumer := taskflow.NewConsumer(taskflow.ConsumerConfig{
		StreamQ:     taskStream,
		TaskRunRepo: taskRunRepo,
		Executor:    taskExec,
		ExecutorID:  "e1",
		Logger:      zlog,
	})
	submitter := subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
		TaskRunRepo: taskRunRepo,
		StreamQ:     taskStream,
		Logger:      zlog,
	})
	poller := taskflow.NewPoller(taskflow.PollerConfig{
		StepRepo:    taskStepRepo,
		TaskRunRepo: taskRunRepo,
		Logger:      zlog,
	})

	// Conversation worker (wired with submitter + poller)
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
	})

	convCtx, convCancel := context.WithCancel(ctx)
	defer convCancel()
	go convConsumer.Start(convCtx)

	taskCtx, taskCancel := context.WithCancel(ctx)
	defer taskCancel()
	go taskConsumer.Start(taskCtx)

	// Pre-create the async request (Pending) + push to the conversation stream, simulating con_reader
	msgID := "msg-phase3"
	asyncReq := &model.CGAsyncRequest{
		AsyncRequestID: "ar-phase3",
		MessageID:      msgID,
		UserID:         1,
		SessionID:      "s1",
		AgentID:        "manager",
		Query:          "生成一张海报",
		Status:         model.AsyncStatusPending,
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, asyncReq))

	// con_reader also creates message_record first (flushChatTextToDB depends on it existing)
	ct := model.NewChatText("生成一张海报")
	ctJSON, _ := json.Marshal(ct)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: msgID,
		UserID:    1,
		AgentID:   "manager",
		SessionID: "s1",
		ChatText:  string(ctJSON),
		Status:    model.MessageStatusInProgress,
	}))

	convBody, _ := json.Marshal(map[string]interface{}{
		"async_request_id": "ar-phase3",
		"msg_id":           msgID,
		"user_id":          1,
		"session_id":       "s1",
		"agent_id":         "manager",
		"query":            "生成一张海报",
	})
	testx.NoError(t, convStream.Push(ctx, msgID, convBody))

	// Wait for the manager to produce the plan → task_run persisted
	testx.Eventually(t, func() bool {
		// Any task_run means the plan chain works
		var count int64
		db.Model(&model.CGTaskRun{}).Count(&count)
		return count > 0
	}, 10*time.Second, 100*time.Millisecond, "the manager should produce a task_run")

	// Wait for the subtask to finish → task_run Finished
	testx.Eventually(t, func() bool {
		var runs []model.CGTaskRun
		db.Find(&runs)
		for _, r := range runs {
			if r.Status == model.TaskRunStatusFinished {
				return true
			}
		}
		return false
	}, 10*time.Second, 100*time.Millisecond, "the subtask should finish")

	// Verify task_step persistence (agent step + tool step)
	var steps []model.CGTaskStep
	db.Find(&steps)
	testx.NotEmpty(t, steps, "there should be at least the agent step")
	testx.Equal(t, model.TaskStepStatusFinished, steps[0].Status)
	testx.Contains(t, steps[0].StepOutput, "final_product")

	// Verify the chunk backflow (task-step / task-done frames + EndTurn closing)
	testx.Eventually(t, func() bool {
		chunks, _, _, err := chunkStore.After(ctx, msgID, 0)
		testx.NoError(t, err)
		hasEnd := false
		for _, c := range chunks {
			if c.EndTurn {
				hasEnd = true
			}
		}
		return hasEnd
	}, 10*time.Second, 100*time.Millisecond, "an EndTurn chunk should exist")

	// Check backflow frame types: task-step and task-done must be present (the core of the Phase 3 milestone)
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
	testx.Contains(t, types, "task-step", "task-step progress frames should flow back")
	testx.Contains(t, types, "task-done", "the task-done terminal frame should flow back")
	testx.Contains(t, types, "message-complete")

	// Verify the async request's terminal state: the EndTurn chunk is written before MarkTerminal(Completed);
	// the worker commits the terminal state in a separate goroutine, so poll instead of asserting directly (eliminating the race).
	testx.Eventually(t, func() bool {
		finished, err := asyncRepo.GetByMessageID(ctx, msgID)
		return err == nil && finished.Status == model.AsyncStatusCompleted
	}, 10*time.Second, 100*time.Millisecond, "the async request should be marked Completed")
}

var _ = zap.S()
