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

// TestAsyncTraceE2E verifies the async full-chain trace: an in-memory exporter asserts span chaining.
// It reuses phase3_test.go's chain construction (memory infra + sqlite).
// The test first sets the global provider to the in-memory exporter (worker/executor read the global via otel.Tracer());
// app.New is not called, so otelx.Init never overwrites the global.
// It also sets the global W3C propagator: the global default is no-op, and without it tracex.InjectCarrier produces no traceparent.
func TestAsyncTraceE2E(t *testing.T) {
	// 0. Set the global provider + W3C propagator (carrier propagation depends on the propagator);
	//    restore the old values with defer to avoid polluting global state for other tests in the same process.
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

	// 1. Build the full worker+executor chain (same as phase3_test.go)
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

	// 2. Pre-create async_request + message_record and push a stream message carrying traceparent
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

	// Create a parent span with the in-memory tracer and inject the carrier, simulating the HTTP chain
	parentCtx, parentSpan := tp.Tracer("test").Start(context.Background(), "http.conversation")
	carrier := tracex.InjectCarrier(parentCtx)
	testx.NotEmpty(t, carrier["traceparent"], "the parent span should produce a traceparent")

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

	// 3. Wait for task_run Finished
	testx.Eventually(t, func() bool {
		var runs []model.CGTaskRun
		db.Find(&runs)
		for _, r := range runs {
			if r.Status == model.TaskRunStatusFinished {
				return true
			}
		}
		return false
	}, 15*time.Second, 200*time.Millisecond, "the subtask should finish")
	parentSpan.End()

	// 4. Assert the in-memory exporter received worker.process + task.process, in the same trace as the HTTP parent.
	// The in-memory exporter batches at 10ms; worker.process is the outer span (finishing last), so poll until flushed.
	var workerSpan, taskSpan sdktrace.ReadOnlySpan
	testx.Eventually(t, func() bool {
		spans := exp.GetSpans()
		workerSpan = findSpanByName(spans, "worker.process")
		taskSpan = findSpanByName(spans, "task.process")
		return workerSpan != nil && taskSpan != nil
	}, 5*time.Second, 50*time.Millisecond, "worker.process + task.process spans should be received")

	spans := exp.GetSpans()
	var names []string
	for _, s := range spans {
		names = append(names, s.Name())
	}
	testx.Contains(t, names, "worker.process", "a worker.process span should exist")
	testx.Contains(t, names, "task.process", "a task.process span should exist")

	testx.NotNil(t, workerSpan, "the worker.process span should exist")
	testx.NotNil(t, taskSpan, "the task.process span should exist")
	testx.Equal(t, workerSpan.SpanContext().TraceID(), taskSpan.SpanContext().TraceID(),
		"worker and task should belong to the same trace")
	testx.Equal(t, parentSpan.SpanContext().TraceID(), workerSpan.SpanContext().TraceID(),
		"worker.process should inherit the HTTP parent's trace")

	// Locks in the taskPayload real-msg_id fix: the task.process span should carry the real messageID attribute
	// (previously it was a "task-{id}" guid, so the task chain could not be correlated with the conversation chain).
	gotMsgID := spanAttr(taskSpan, "msg_id")
	testx.Equal(t, msgID, gotMsgID, "the task.process span's msg_id should be the real messageID")
}

// spanAttr returns the string attribute of a span for the given key (empty string when absent).
func spanAttr(s sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

// findSpanByName looks up a span by name among those collected by the in-memory exporter (nil when not found).
func findSpanByName(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}
