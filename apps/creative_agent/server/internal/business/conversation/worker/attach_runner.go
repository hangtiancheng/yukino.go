package worker

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
)

// AttachSink is the callback for frames produced by attach. Implementations handle the history-completion marker themselves.
type AttachSink func(key string, raw string)

// AttachOptions controls RunAttach's behavior.
type AttachOptions struct {
	// LastSeq is the resume point (the number of chunks the frontend has consumed).
	LastSeq int64
	// When AsyncRepo is non-nil, terminal-state polling is enabled (checking whether the task died when no new chunks arrive).
	AsyncRepo repository.AsyncRequestRepository
	// EndPush is the sentinel frame pushed on EndTurn or a terminal state.
	EndPush AttachSink
	// ReconnectPush pushes the reconnect frame when replay starts.
	ReconnectPush AttachSink
	// InterruptPush pushes the interruption hint frame when the task is terminal but has no EndTurn.
	InterruptPush AttachSink
	// PollInterval is the fallback polling interval, default 2s.
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

	// Only resume pulls (lastSeq>0) push the reconnect frame; brand-new requests do not,
	// avoiding the frontend mistaking the "first message" for "history completion".
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
			// New chunks arrived; keep looping
		case <-time.After(interval):
			if opts.AsyncRepo != nil {
				done, _ := isAsyncTerminal(ctx, opts.AsyncRepo, msgID)
				if done {
					// Task terminal but no EndTurn arrived → interruption hint
					if opts.InterruptPush != nil {
						opts.InterruptPush(msgID, `{"type":"reconnect","msgId":"`+msgID+`","history":true,"interrupted":true}`)
					}
					emit(msgID, "__FLOW_END__", opts.EndPush, sink)
					return nil
				}
			}
			// Not terminal → keep waiting
		}
	}
}

// emit dispatches raw frames to callbacks. `__FLOW_END__` is a control sentinel that only goes to endPush;
// it is dropped when endPush is empty, avoiding leaks into the content sink. All other frames go to the content sink.
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
