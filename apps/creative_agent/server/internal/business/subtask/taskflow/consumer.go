package taskflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/executor"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// Consumer 是子任务消费者（对应真实系统 executor/consumer.go）。
//
// 可靠性比对话管道（③）轻——子任务是纯后台任务，没有连接在等：
//   - MsgID 锁去重（同一消息只有一个 worker 处理）
//   - CAS 抢占 cg_task_run（Pending→Running）
//   - 超时/异常不 ACK → stream 消息 pending → 由任务 stream 的 recovery 兜底重投
//
// 无心跳、无僵尸扫描（对话管道的 RecoveryScanner 只扫 cg_async_request，不扫 task_run）。
type Consumer struct {
	streamQ     infra.StreamQueue
	taskRunRepo repository.TaskRunRepository
	executor    *subtaskexecutor.TaskExecutor // 执行体（Step 3.4），nil 时只做抢占验证
	executorID  string                        // 本 consumer 唯一标识（CAS 抢占的 owner 记录）
	logger      *zap.Logger
}

// ConsumerConfig 持有 Consumer 的依赖。
type ConsumerConfig struct {
	StreamQ     infra.StreamQueue
	TaskRunRepo repository.TaskRunRepository
	Executor    *subtaskexecutor.TaskExecutor
	ExecutorID  string
	Logger      *zap.Logger
}

// NewConsumer 创建子任务消费者。
func NewConsumer(cfg ConsumerConfig) *Consumer {
	if cfg.ExecutorID == "" {
		cfg.ExecutorID = "executor-1"
	}
	return &Consumer{
		streamQ:     cfg.StreamQ,
		taskRunRepo: cfg.TaskRunRepo,
		executor:    cfg.Executor,
		executorID:  cfg.ExecutorID,
		logger:      cfg.Logger,
	}
}

// Start 开始消费循环。阻塞直到 ctx 取消。
func (c *Consumer) Start(ctx context.Context) {
	c.logger.Info("task consumer started", zap.String("executorID", c.executorID))
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("task consumer stopped")
			return
		default:
		}

		msg, err := c.streamQ.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Error("task consumer receive error", zap.Error(err))
			continue
		}
		if msg == nil {
			continue
		}

		// 并发处理，慢任务不阻塞轮询
		go c.handleTask(ctx, msg)
	}
}

// handleTask 处理单条子任务消息。
func (c *Consumer) handleTask(ctx context.Context, msg *infra.StreamMessage) {
	var payload subtaskexecutor.TaskPayload
	if err := json.Unmarshal(msg.Body, &payload); err != nil {
		c.logger.Error("task consumer: unmarshal payload error", zap.Error(err))
		_ = c.streamQ.Ack(ctx, msg.ID) // Poison message, ack to avoid blocking
		return
	}

	// 从 stream 恢复 trace（跨进程透传）。
	ctx = tracex.ContextFromCarrier(ctx, map[string]string{
		tracex.TraceParentKey: payload.TraceParent,
	})

	// 包 task.process span：把子任务处理（抢占/执行/落库）挂进 HTTP 恢复出的 trace。
	ctx, span := otel.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/executor").Start(ctx, "task.process")
	defer span.End()
	span.SetAttributes(
		attribute.Int64("task_run_id", payload.TaskRunID),
		attribute.String("msg_id", payload.MsgID),
	)
	// 入口派生 logger 放在开 span 之后：日志的 trace_id/span_id 指向 task.process 本身。
	logger := tracex.Logger(c.logger, ctx)
	logger.Info("task consumer: processing message",
		zap.Int64("taskRunID", payload.TaskRunID),
		zap.String("msgID", payload.MsgID),
		zap.String("executorID", c.executorID))

	// Step 1: 主从延迟重试——Submitter 刚 INSERT 完就 Push，避免读到 nil。
	run, err := c.getRunWithRetry(ctx, payload.TaskRunID)
	if err != nil {
		logger.Error("task consumer: get task_run error",
			zap.Int64("taskRunID", payload.TaskRunID), zap.Error(err))
		return // 不 ACK，等重投
	}

	// Step 2: 状态机 + CAS 抢占
	acquired, skip := c.preempt(ctx, run)
	if skip {
		_ = c.streamQ.Ack(ctx, msg.ID)
		return
	}
	if !acquired {
		logger.Warn("task consumer: CAS 抢占失败（已有执行者）",
			zap.Int64("taskRunID", payload.TaskRunID))
		_ = c.streamQ.Ack(ctx, msg.ID)
		return
	}

	// Step 3: 交给 TaskExecutor 执行（含 NeedWait 分流 + 结果落库）
	if c.executor == nil {
		// 未接线执行体：仅验证抢占后直接终态（供 Step 3.3 单元测试使用）
		_ = c.taskRunRepo.MarkTerminal(ctx, run.TaskRunID, model.TaskRunStatusFinished, "")
		_ = c.streamQ.Ack(ctx, msg.ID)
		return
	}

	shouldAck := c.executor.Execute(ctx, run)
	if shouldAck {
		_ = c.streamQ.Ack(ctx, msg.ID)
	} else {
		logger.Warn("task consumer: 执行未完成（需重投），不 ACK",
			zap.Int64("taskRunID", payload.TaskRunID))
	}
}

// getRunWithRetry 最多重试 3 次 ×200ms，防读到 nil（Submitter 刚建表就投递）。
func (c *Consumer) getRunWithRetry(ctx context.Context, taskRunID int64) (*model.CGTaskRun, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		run, err := c.taskRunRepo.GetByTaskRunID(ctx, taskRunID)
		if err == nil {
			return run, nil
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("get task_run after 3 retries: %w", lastErr)
}

// preempt 对 task_run 做 CAS 抢占。返回 (是否抢到, 是否应跳过)。
//
//	switch status:
//	  Pending          → 继续抢占（Pending→Running）
//	  Finished/Failed  → 幂等返回，跳过
//	  Running          → 判 zombie：更新耗时过长视为崩溃，重置为 Pending 重新抢占；
//	                     否则认为有活跃执行者，跳过
func (c *Consumer) preempt(ctx context.Context, run *model.CGTaskRun) (acquired bool, skip bool) {
	switch run.Status {
	case model.TaskRunStatusFinished, model.TaskRunStatusFailed:
		return false, true // 幂等返回

	case model.TaskRunStatusRunning:
		// zombie 判据：距 UpdateTime 超过 workerTimeout（此处 5min）认为执行者崩溃
		workerTimeout := 5 * time.Minute
		if time.Since(time.UnixMilli(run.UpdateTime)) < workerTimeout {
			return false, true // 有活跃执行者，跳过
		}
		// zombie：重置为 Pending 重新抢占
		ok, err := c.taskRunRepo.UpdateCAS(ctx, run.TaskRunID, model.TaskRunStatusRunning, map[string]any{
			"status": model.TaskRunStatusPending,
		})
		if err != nil || !ok {
			return false, true
		}
		// 落回 Pending 后继续往下走（Pending 分支）
		run.Status = model.TaskRunStatusPending
	}

	// Pending → Running（CAS 抢占）
	ok, err := c.taskRunRepo.UpdateCAS(ctx, run.TaskRunID, model.TaskRunStatusPending, map[string]any{
		"status":      model.TaskRunStatusRunning,
		"consumer_ip": c.executorID,
		"start_time":  time.Now().UnixMilli(),
	})
	if err != nil || !ok {
		return false, false
	}
	return true, false
}
