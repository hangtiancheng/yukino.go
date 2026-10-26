package worker

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"
)

// RecoveryScanner periodically scans the cg_async_request table for zombies
// (InProgress records with expired heartbeats) and applies one of two recovery
// strategies.
//
// Scan interval: 30s. Heartbeat expiry: 15s (3×5s heartbeat interval).
// Max recovery delay: ~45s (15s expiry + 30s scan).
//
// Recovery strategies:
//   - Strategy A (HasEndTurn): ReAct finished, just missing DB finalization.
//     → Rebuild chat_text from Redis chunks (tryRecoverFromRedis), then
//     MarkTerminal(Completed). If chunks expired and rebuild is impossible,
//     MarkTerminal(Failed) to close the loop.
//   - Strategy B (no EndTurn): Worker crashed mid-execution.
//     → ResetForRetry (InProgress→Pending) + StreamQueue.Requeue for fresh execution.
type RecoveryScanner struct {
	asyncRepo  repository.AsyncRequestRepository  // 查僵尸 + 翻状态
	msgRepo    repository.MessageRecordRepository // 落库重建（策略 A 写回 chat_text）
	chunkStore infra.ChunkStore                   // 查 EndTurn 标记（策略分发依据）+ 重建数据源
	streamQ    infra.StreamQueue                  // 重投消息（策略 B）
	logger     *zap.Logger
}

// RecoveryConfig holds dependencies for creating a RecoveryScanner.
type RecoveryConfig struct {
	AsyncRepo  repository.AsyncRequestRepository
	MsgRepo    repository.MessageRecordRepository
	ChunkStore infra.ChunkStore
	StreamQ    infra.StreamQueue
	Logger     *zap.Logger
}

// NewRecoveryScanner creates a zombie recovery scanner.
func NewRecoveryScanner(cfg RecoveryConfig) *RecoveryScanner {
	return &RecoveryScanner{
		asyncRepo:  cfg.AsyncRepo,
		msgRepo:    cfg.MsgRepo,
		chunkStore: cfg.ChunkStore,
		streamQ:    cfg.StreamQ,
		logger:     cfg.Logger,
	}
}

// Start begins the recovery scan loop. Periodically scans for zombies
// and applies recovery strategies.
func (r *RecoveryScanner) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	r.logger.Info("recovery scanner started")
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("recovery scanner stopped")
			return
		case <-ticker.C:
			r.scan(ctx)
		}
	}
}

// scan performs one round of zombie detection and recovery.
func (r *RecoveryScanner) scan(ctx context.Context) {
	r.ScanOnce(ctx)
}

// ScanOnce 执行一轮僵尸检测与恢复（单次，供定时循环 Start 与测试直接调用）。
func (r *RecoveryScanner) ScanOnce(ctx context.Context) {
	// Heartbeat expires when heartbeat_at < now - 15s
	expiry := time.Now().Add(-15 * time.Second)
	zombies, err := r.asyncRepo.ListZombies(ctx, expiry)
	if err != nil {
		r.logger.Error("recovery: scan failed", zap.Error(err))
		return
	}

	for _, z := range zombies {
		r.logger.Warn("recovery: zombie detected",
			zap.String("msgID", z.MessageID),
			zap.Int("attemptCount", z.AttemptCount))

		// Exceeds max attempts → direct to Failed
		if z.AttemptCount >= model.MaxAsyncAttempt {
			r.logger.Warn("recovery: max attempts exceeded",
				zap.String("msgID", z.MessageID))
			_ = r.asyncRepo.MarkTerminal(ctx, z.ID, model.AsyncStatusFailed)
			continue
		}

		// Strategy A: chunk has EndTurn → ReAct finished, just mark terminal
		hasEnd, err := r.chunkStore.HasEndTurn(ctx, z.MessageID)
		if err != nil {
			r.logger.Error("recovery: HasEndTurn failed",
				zap.String("msgID", z.MessageID),
				zap.Error(err))
			continue
		}
		if hasEnd {
			// 重建落库（tryRecoverFromRedis 语义）：chunk 里有完整产出，写回 message_record。
			// 落库是收口动作，此处仅兜底失败场景；worker 正常路径已落库，重复重建幂等无害。
			if r.msgRepo != nil {
				if err := RebuildChatTextFromChunks(ctx, r.msgRepo, r.chunkStore, z.MessageID); err != nil {
					// 重建失败（Redis 读失败 / MySQL 写失败）：不能标 Completed 丢数据，
					// 标 Failed 收口 + 保留 chunk 供人工/重扫处置。
					r.logger.Error("recovery: strategy A — rebuild chat_text failed, marking Failed",
						zap.String("msgID", z.MessageID), zap.Error(err))
					_ = r.asyncRepo.MarkTerminal(ctx, z.ID, model.AsyncStatusFailed)
					continue
				}
				r.logger.Info("recovery: strategy A — chat_text rebuilt from chunks",
					zap.String("msgID", z.MessageID))
			} else {
				// msgRepo 未装配：无重建能力，退化为旧行为（仅翻状态），chunk 过期后数据仍丢。
				r.logger.Warn("recovery: strategy A — msgRepo 未装配，仅翻状态不重建",
					zap.String("msgID", z.MessageID))
			}
			r.logger.Info("recovery: strategy A — EndTurn found, marking Completed",
				zap.String("msgID", z.MessageID))
			_ = r.asyncRepo.MarkTerminal(ctx, z.ID, model.AsyncStatusCompleted)
			continue
		}

		// Strategy B: no EndTurn → reset for retry + requeue
		r.logger.Info("recovery: strategy B — no EndTurn, requeueing",
			zap.String("msgID", z.MessageID))

		// Reset state to Pending (prevents re-scanning the same zombie)
		if err := r.asyncRepo.ResetForRetry(ctx, z.ID); err != nil {
			r.logger.Error("recovery: ResetForRetry failed",
				zap.String("msgID", z.MessageID),
				zap.Error(err))
			continue
		}

		// Requeue into stream
		body, _ := json.Marshal(map[string]interface{}{
			"async_request_id": z.AsyncRequestID,
			"msg_id":           z.MessageID,
			"user_id":          z.UserID,
			"session_id":       z.SessionID,
			"agent_id":         z.AgentID,
			"query":            z.Query,
			"traceparent":      tracex.InjectCarrier(ctx)["traceparent"],
		})
		if err := r.streamQ.Requeue(ctx, z.MessageID, body); err != nil {
			r.logger.Error("recovery: requeue failed",
				zap.String("msgID", z.MessageID),
				zap.Error(err))
		}
	}
}
