package worker

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
)

// RunAttach subscribes to Redis chunks for msgID and replays each chunk
// into the flow context via the given reconnectKey. It blocks until EndTurn,
// terminal status, or context cancellation. lastSeq resumes from a breakpoint.
func RunAttach(ctx context.Context, msgID string, chunkStore infra.ChunkStore,
	flowCtx *seda.Context, reconnectKey string, lastSeq int64,
	asyncRepo repository.AsyncRequestRepository) error {

	return RunAttachCore(ctx, msgID, chunkStore, AttachOptions{
		LastSeq:       lastSeq,
		AsyncRepo:     asyncRepo,
		EndPush:       func(_ string, raw string) { flowCtx.Push(reconnectKey, raw) },
		ReconnectPush: func(_ string, raw string) { flowCtx.Push(reconnectKey, raw) },
		InterruptPush: func(_ string, raw string) { flowCtx.Push(reconnectKey, raw) },
	}, func(_ string, raw string) {
		// 原样透传 chunk 里的 SSEFrame JSON。Worker 侧产出的是完整帧
		// （含 msgId / textId），con_writer 会提取 type 作事件名。
		flowCtx.Push(reconnectKey, raw)
	})
}
