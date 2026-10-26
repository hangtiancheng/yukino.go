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

// newPhase3TestDB 创建内存 sqlite，注册 Phase 3 全部表。
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

// TestPhase3_EndToEnd_PlanToPoll 验证 Phase 3 完整链路：
// 管家产出 new_agents_plan → Submitter 投递 → Consumer 消费 →
// TaskExecutor 执行 → Poller 轮询 cg_task_step 回流。
func TestPhase3_EndToEnd_PlanToPoll(t *testing.T) {
	ctx := context.Background()
	zlog := logger.New("info")
	db := newPhase3TestDB(t)

	asyncRepo := repository.NewAsyncRequestRepository(db)
	msgRepo := repository.NewMessageRecordRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	taskStepRepo := repository.NewTaskStepRepository(db)

	// 两条 stream：对话 + 子任务
	convStream := infra.NewMemoryStreamQueue("cg:stream:conversation", "cg:workers", "w1")
	taskStream := infra.NewMemoryStreamQueue("cg:stream:task", "cg:executors", "e1")
	chunkStore := infra.NewMemoryChunkStore()
	defer chunkStore.Close()

	// 组装 Phase 3 组件
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

	// 对话 worker（接 submitter + poller）
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

	// 预建异步请求（Pending）+ 投递对话 stream，模拟 con_reader
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

	// con_reader 还会先建 message_record（flushChatTextToDB 依赖它存在）
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

	// 等待管家产出 plan → task_run 落库
	testx.Eventually(t, func() bool {
		// 有任意 task_run 即说明 plan 链路打通
		var count int64
		db.Model(&model.CGTaskRun{}).Count(&count)
		return count > 0
	}, 10*time.Second, 100*time.Millisecond, "管家应产出 task_run")

	// 等待子任务执行完成 → task_run Finished
	testx.Eventually(t, func() bool {
		var runs []model.CGTaskRun
		db.Find(&runs)
		for _, r := range runs {
			if r.Status == model.TaskRunStatusFinished {
				return true
			}
		}
		return false
	}, 10*time.Second, 100*time.Millisecond, "子任务应执行完成")

	// 验证 task_step 落库（agent step + tool step）
	var steps []model.CGTaskStep
	db.Find(&steps)
	testx.NotEmpty(t, steps, "应至少有 agent step")
	testx.Equal(t, model.TaskStepStatusFinished, steps[0].Status)
	testx.Contains(t, steps[0].StepOutput, "final_product")

	// 验证 chunk 回流（含 task-step / task-done 帧 + EndTurn 收尾）
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
	}, 10*time.Second, 100*time.Millisecond, "应有 EndTurn chunk")

	// 校验回流帧类型：应包含 task-step 与 task-done（Phase 3 milestone 核心）
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
	testx.Contains(t, types, "task-step", "应回流 task-step 进度帧")
	testx.Contains(t, types, "task-done", "应回流 task-done 终态帧")
	testx.Contains(t, types, "message-complete")

	// 验证异步请求终态
	finished, err := asyncRepo.GetByMessageID(ctx, msgID)
	testx.NoError(t, err)
	testx.Equal(t, model.AsyncStatusCompleted, finished.Status)
}

var _ = zap.S()
