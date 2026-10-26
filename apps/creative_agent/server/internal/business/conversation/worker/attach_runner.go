package worker

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
)

// AttachSink 是 attach 产出的帧回调。历史补全标记由实现方自行处理。
type AttachSink func(key string, raw string)

// AttachOptions 控制 RunAttach 的行为。
type AttachOptions struct {
	// LastSeq 是从断点续拉的起点（前端已消费的 chunk 数）。
	LastSeq int64
	// AsyncRepo 非 nil 时启用终态轮询（无新 chunk 时检查任务是否死了）。
	AsyncRepo repository.AsyncRequestRepository
	// EndPush 是收到 EndTurn 或终态时推送的哨兵帧。
	EndPush AttachSink
	// ReconnectPush 在回放开始时推送 reconnect 帧。
	ReconnectPush AttachSink
	// InterruptPush 在任务终态但无 EndTurn 时推送中断提示帧。
	InterruptPush AttachSink
	// PollInterval 是兜底轮询间隔，默认 2s。
	PollInterval time.Duration
}

// RunAttachCore subscribes to Redis chunks for msgID and replays each chunk
// via sink. It resumes from opts.LastSeq, blocks until EndTurn / terminal
// status / context cancel. This is the decoupled core; the operator wrapper
// in attach.go wires it to the flowcontext.
func RunAttachCore(ctx context.Context, msgID string, chunkStore infra.ChunkStore, opts AttachOptions, sink AttachSink) error {
	interval := opts.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	cursor := opts.LastSeq

	// 断点续拉（lastSeq>0）才推 reconnect 帧；全新请求不推，
	// 避免前端把"首条消息"误判为"历史补全"。
	if opts.ReconnectPush != nil && opts.LastSeq > 0 {
		opts.ReconnectPush(msgID, `{"type":"reconnect","msgId":"`+msgID+`","history":true}`)
	}

	for {
		chunks, newCursor, notify, err := chunkStore.After(ctx, msgID, cursor)
		if err != nil {
			return err
		}
		cursor = newCursor

		for _, c := range chunks {
			if c.EndTurn {
				emit(msgID, "__FLOW_END__", opts.EndPush, sink)
				return nil
			}
			emit(msgID, c.Payload, nil, sink)
		}

		select {
		case <-ctx.Done():
			emit(msgID, "__FLOW_END__", opts.EndPush, sink)
			return ctx.Err()
		case <-notify:
			// 有新 chunk，继续循环
		case <-time.After(interval):
			if opts.AsyncRepo != nil {
				done, _ := isAsyncTerminal(ctx, opts.AsyncRepo, msgID)
				if done {
					// 任务终态但没等到 EndTurn → 中断提示
					if opts.InterruptPush != nil {
						opts.InterruptPush(msgID, `{"type":"reconnect","msgId":"`+msgID+`","history":true,"interrupted":true}`)
					}
					emit(msgID, "__FLOW_END__", opts.EndPush, sink)
					return nil
				}
			}
			// 未终态 → 继续等待
		}
	}
}

// emit 将 raw 帧分发到回调。`__FLOW_END__` 是控制哨兵，只走 endPush；
// endPush 为空时丢弃，避免泄漏到内容 sink。其余帧走内容 sink。
func emit(key, raw string, endPush AttachSink, sink AttachSink) {
	if raw == "__FLOW_END__" {
		if endPush != nil {
			endPush(key, raw)
		}
		return
	}
	if sink != nil {
		sink(key, raw)
	}
}

// isAsyncTerminal returns true if the async request for msgID is Completed or Failed.
func isAsyncTerminal(ctx context.Context, repo repository.AsyncRequestRepository, msgID string) (bool, error) {
	req, err := repo.GetByMessageID(ctx, msgID)
	if err != nil {
		if err == repository.ErrAsyncRequestNotFound {
			return false, nil
		}
		return false, err
	}
	switch req.Status {
	case model.AsyncStatusCompleted, model.AsyncStatusFailed:
		return true, nil
	default:
		return false, nil
	}
}
