package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"

	"go.uber.org/zap"
)

// runHeartbeat periodically refreshes both the Redis heartbeat lock (SETEX 30s)
// and the MySQL heartbeat_at column. It stops when the done channel is closed
// or ctx is cancelled.
func runHeartbeat(
	ctx context.Context,
	done <-chan struct{},
	locker infra.Locker,
	asyncRepo repository.AsyncRequestRepository,
	msgID string,
	asyncRequestID int64,
	workerID string,
	logger *zap.Logger,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Refresh lock TTL
			hbKey := fmt.Sprintf("cg:hb:%s", msgID)
			if err := locker.Expire(ctx, hbKey, 30*time.Second); err != nil {
				logger.Warn("heartbeat: lock expire failed",
					zap.String("msgID", msgID),
					zap.Error(err))
			}

			// Refresh MySQL heartbeat_at
			if err := asyncRepo.UpdateHeartbeat(ctx, asyncRequestID); err != nil {
				logger.Warn("heartbeat: mysql update failed",
					zap.Int64("asyncRequestID", asyncRequestID),
					zap.Error(err))
			}
		}
	}
}
