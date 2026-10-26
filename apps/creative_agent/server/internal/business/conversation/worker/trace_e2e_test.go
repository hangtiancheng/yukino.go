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
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestAsyncTraceE2E 验证异步全链路 trace：内存 exporter 断言 span 串接。
// 复用 phase3_test.go 的链路构建方式（memory infra + sqlite）。
// 测试先设全局 provider 为内存 exporter（worker/executor 用 otel.Tracer() 读全局），
// 不调用 app.New，因此不会触发 otelx.Init 覆盖全局。
// 同时设全局 W3C propagator：默认全局是 no-op，不设则 tracex.InjectCarrier 产不出 traceparent。
func TestAsyncTraceE2E(t *testing.T) {
	// 0. 设全局 provider + W3C propagator（carrier 透传依赖传播器），
	//    用 defer 恢复旧值，避免污染同一测试进程里其他测试的全局状态。
	tp, exp := otelx.NewMemoryExporter()
	oldTP := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	defer func() {
		otel.SetTracerProvider(oldTP)
		otel.SetTextMapPropagator(oldProp)
		_ = tp.Shutdown(context.Background())
	}()

	// 1. 构建 worker+executor 全链路（同 phase3_test.go）
	ctx := context.Background()
	zlog := logger.New("error")
	db := newPhase3TestDB(t)

	asyncRepo := repository.NewAsyncRequestRepository(db)
	msgRepo := repository.NewMessageRecordRepository(db)
	taskRunRepo := repository.NewTaskRunRepository(db)
	taskStepRepo := repository.NewTaskStepRepository(db)

	convStream := infra.NewMemoryStreamQueue("cg:stream:conversation", "cg:workers", "w1")
	taskStream := infra.NewMemoryStreamQueue("cg:stream:task", "cg:executors", "e1")
	chunkStore := infra.NewMemoryChunkStore()
	defer chunkStore.Close()

	tools := appkittools.NewSubAgentToolRegistry()
	taskExec := subtaskexecutor.NewTaskExecutor(subtaskexecutor.TaskExecutorConfig{
		TaskRunRepo: taskRunRepo, StepRepo: taskStepRepo, Tools: tools, Logger: zlog,
	})
	taskConsumer := taskflow.NewConsumer(taskflow.ConsumerConfig{
		StreamQ: taskStream, TaskRunRepo: taskRunRepo, Executor: taskExec, ExecutorID: "e1", Logger: zlog,
	})
	submitter := subtaskexecutor.NewSubmitter(subtaskexecutor.SubmitterConfig{
		TaskRunRepo: taskRunRepo, StreamQ: taskStream, Logger: zlog,
	})
	poller := taskflow.NewPoller(taskflow.PollerConfig{
		StepRepo: taskStepRepo, TaskRunRepo: taskRunRepo, Logger: zlog,
	})
	convConsumer := worker.NewConsumer(worker.ConsumerConfig{
		StreamQ: convStream, ChunkStore: chunkStore, Lock: infra.NewMemoryLocker(),
		AsyncRepo: asyncRepo, MsgRepo: msgRepo, WorkerID: "w1", Logger: zlog,
		TaskSubmitter: submitter, TaskPoller: poller,
	})

	convCtx, convCancel := context.WithCancel(ctx)
	defer convCancel()
	go convConsumer.Start(convCtx)
	taskCtx, taskCancel := context.WithCancel(ctx)
	defer taskCancel()
	go taskConsumer.Start(taskCtx)

	// 2. 预建 async_request + message_record，投递带 traceparent 的 stream 消息
	msgID := "msg-trace-e2e"
	testx.NoError(t, asyncRepo.InsertPending(ctx, &model.CGAsyncRequest{
		AsyncRequestID: "ar-trace-e2e", MessageID: msgID, UserID: 1,
		SessionID: "s1", AgentID: "manager", Query: "生成一张海报",
		Status: model.AsyncStatusPending,
	}))
	ct := model.NewChatText("生成一张海报")
	ctJSON, _ := json.Marshal(ct)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: msgID, UserID: 1, AgentID: "manager", SessionID: "s1",
		ChatText: string(ctJSON), Status: model.MessageStatusInProgress,
	}))

	// 用内存 tracer 建一个 parent span，注入 carrier 模拟 HTTP 链路
	parentCtx, parentSpan := tp.Tracer("test").Start(context.Background(), "http.conversation")
	carrier := tracex.InjectCarrier(parentCtx)
	testx.NotEmpty(t, carrier["traceparent"], "parent span 应产出 traceparent")

	convBody, _ := json.Marshal(map[string]interface{}{
		"async_request_id": "ar-trace-e2e",
		"msg_id":           msgID,
		"user_id":          1,
		"session_id":       "s1",
		"agent_id":         "manager",
		"query":            "生成一张海报",
		"traceparent":      carrier["traceparent"],
	})
	testx.NoError(t, convStream.Push(ctx, msgID, convBody))

	// 3. 等待 task_run Finished
	testx.Eventually(t, func() bool {
		var runs []model.CGTaskRun
		db.Find(&runs)
		for _, r := range runs {
			if r.Status == model.TaskRunStatusFinished {
				return true
			}
		}
		return false
	}, 15*time.Second, 200*time.Millisecond, "子任务应执行完成")
	parentSpan.End()

	// 4. 断言内存 exporter 收到 worker.process + task.process，且与 HTTP parent 同一 trace。
	// 内存 exporter 用 10ms 批处理，worker.process 是外层 span（结束最晚），需轮询等待 flush。
	var workerSpan, taskSpan sdktrace.ReadOnlySpan
	testx.Eventually(t, func() bool {
		spans := exp.GetSpans()
		workerSpan = findSpanByName(spans, "worker.process")
		taskSpan = findSpanByName(spans, "task.process")
		return workerSpan != nil && taskSpan != nil
	}, 5*time.Second, 50*time.Millisecond, "应收到 worker.process + task.process span")

	spans := exp.GetSpans()
	var names []string
	for _, s := range spans {
		names = append(names, s.Name())
	}
	testx.Contains(t, names, "worker.process", "应有 worker.process span")
	testx.Contains(t, names, "task.process", "应有 task.process span")

	testx.NotNil(t, workerSpan, "worker.process span 应存在")
	testx.NotNil(t, taskSpan, "task.process span 应存在")
	testx.Equal(t, workerSpan.SpanContext().TraceID(), taskSpan.SpanContext().TraceID(),
		"worker 与 task 应属同一 trace")
	testx.Equal(t, parentSpan.SpanContext().TraceID(), workerSpan.SpanContext().TraceID(),
		"worker.process 应继承 HTTP parent 的 trace")

	// 锁定 taskPayload 真实 msg_id 修复：task.process span 应带真实 messageID 属性
	//（此前是 "task-{id}" guid，task 链路无法关联 conversation 链路）。
	gotMsgID := spanAttr(taskSpan, "msg_id")
	testx.Equal(t, msgID, gotMsgID, "task.process span 的 msg_id 应为真实 messageID")
}

// spanAttr 返回 span 指定 key 的字符串属性（不存在返回 ""）。
func spanAttr(s sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

// findSpanByName 从内存 exporter 收集的 span 里按名字查找（未找到返回 nil）。
func findSpanByName(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}
